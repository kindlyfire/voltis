package me.tijlvdb.voltis.ui.grid

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.Pager
import androidx.paging.PagingConfig
import androidx.paging.PagingData
import androidx.paging.PagingSource
import androidx.paging.cachedIn
import dagger.assisted.Assisted
import dagger.assisted.AssistedFactory
import dagger.assisted.AssistedInject
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.stateIn
import kotlin.random.Random
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.Content
import java.io.IOException
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.api.attemptResult
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.data.auth.AppScope
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.downloads.DownloadStore
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.content.ContentListParams
import me.tijlvdb.voltis.data.content.ContentPagingSource
import me.tijlvdb.voltis.data.content.ContentRepository
import me.tijlvdb.voltis.data.content.ContentSort
import me.tijlvdb.voltis.data.content.DownloadedCatalog
import me.tijlvdb.voltis.data.net.isUnreachable
import me.tijlvdb.voltis.data.content.GridFilters
import me.tijlvdb.voltis.data.content.PagedContent
import me.tijlvdb.voltis.data.content.PagingReach
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.data.reading.BulkRunner
import me.tijlvdb.voltis.data.reading.BulkState
import me.tijlvdb.voltis.data.settings.DeviceSettings
import me.tijlvdb.voltis.data.user.UserRepository
import me.tijlvdb.voltis.domain.catalog.BulkCommand
import me.tijlvdb.voltis.domain.catalog.BulkWhat
import me.tijlvdb.voltis.domain.catalog.DownloadPlan
import me.tijlvdb.voltis.domain.catalog.downloadPlan
import me.tijlvdb.voltis.domain.catalog.FacetKind
import me.tijlvdb.voltis.domain.catalog.GridOptions
import me.tijlvdb.voltis.domain.catalog.Selection
import me.tijlvdb.voltis.domain.catalog.clearPlan
import me.tijlvdb.voltis.domain.catalog.statusPlan
import me.tijlvdb.voltis.domain.comic.ReaderData
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.net.settled
import me.tijlvdb.voltis.data.content.withReading
import me.tijlvdb.voltis.domain.reading.EffectiveReading
import me.tijlvdb.voltis.ui.Refresher
import me.tijlvdb.voltis.ui.UiText

/** What a grid lists. */
sealed interface GridSource {
    data class Library(val id: String) : GridSource

    /** All libraries. A sort seeds the filters (Home's "see all" arrows). */
    data class Browse(val sort: String? = null, val sortOrder: String? = null) : GridSource

    /** A series' contents, in the series' order: the whole list in one request. */
    data class Contents(val seriesId: String) : GridSource

    /** The top-level items with a Discover value. [libraryId] seeds the [FacetScope]. */
    data class Facet(val kind: String, val key: String, val libraryId: String? = null) : GridSource
}

/** What narrows a Discover value's grid: a library, and a role for a person. */
@Serializable
data class FacetScope(val libraryId: String? = null, val role: String? = null)

/** The query a grid's filters apply over. */
fun GridSource.base(filters: GridFilters, scope: FacetScope = FacetScope()): ContentListParams = when (this) {
    is GridSource.Library -> ContentListParams(libraryId = id, parentId = ContentListParams.TOP_LEVEL)
    // The continue sorts list volumes wherever they sit; the others list the top level.
    is GridSource.Browse -> if (filters.listsContinue) ContentListParams() else ContentListParams(parentId = ContentListParams.TOP_LEVEL)
    is GridSource.Contents -> ContentListParams(parentId = seriesId, sort = ContentSort.ORDER)
    is GridSource.Facet -> ContentListParams(
        parentId = ContentListParams.TOP_LEVEL,
        libraryId = scope.libraryId,
        facetKind = kind,
        facet = key,
        facetRole = scope.role.takeIf { kind == FacetKind.PEOPLE },
    )
}

/**
 * Whether [change] can move, add or drop a card of this grid. Any reading or user-data change
 * can in a library. A series' contents follow reading changes of the series and its items, and
 * every user-data change: that event names no series, and a filtered list can gain the item.
 */
fun GridSource.follows(change: CatalogChange): Boolean = when (change) {
    is CatalogChange.ReadingChanged -> this !is GridSource.Contents || seriesId == change.contentId || seriesId == change.parentId
    is CatalogChange.UserDataChanged -> true
    // The server came or went: every screen reloads once (Refresher).
    is CatalogChange.OnlineChanged -> true
    is CatalogChange.PositionSaved, CatalogChange.PreferencesChanged -> false
}

/** A bulk action's dialog. */
enum class BulkDialog { Status, Clear, Download }

/** The bulk download dialog's contents (P4 §6). */
sealed interface DownloadCount {
    data object Counting : DownloadCount

    data class Failed(val text: UiText) : DownloadCount

    /**
     * [owner] is the downloads store the plan was counted for; the download is for it alone. [free] is the bytes
     * available where downloads go; [waitsForWifi]: Wi-Fi only is on and the network is metered.
     */
    data class Ready(val plan: DownloadPlan, val owner: DownloadStore, val free: Long?, val waitsForWifi: Boolean) : DownloadCount
}

/** One grid: its filters, its display options, and its pages or, for a series' contents, its list. */
@OptIn(ExperimentalCoroutinesApi::class)
@HiltViewModel(assistedFactory = GridViewModel.Factory::class)
class GridViewModel @AssistedInject constructor(
    @Assisted private val source: GridSource,
    private val saved: SavedStateHandle,
    private val content: ContentRepository,
    private val settings: DeviceSettings,
    private val users: UserRepository,
    events: CatalogEvents,
    private val downloads: DownloadRepository,
    session: SessionStore,
    private val bulk: BulkRunner,
    private val connectivity: Connectivity,
    @AppScope private val appScope: CoroutineScope,
    private val catalog: DownloadedCatalog,
    reader: ReaderData,
) : ViewModel() {
    @AssistedFactory
    interface Factory {
        fun create(source: GridSource): GridViewModel
    }

    /** The cards' download marks. */
    val badges = downloads.badges

    /** The sorts offered, All libraries' own ones first. None for a series' contents, whose sort is fixed. */
    val sorts = when (source) {
        is GridSource.Browse -> BROWSE_SORTS + LIBRARY_SORTS
        is GridSource.Library, is GridSource.Facet -> LIBRARY_SORTS
        is GridSource.Contents -> emptyList()
    }

    /** In saved state: the filters last while the grid is on a back stack, as the web's URL query does. */
    var filters by mutableStateOf(restore(FILTERS) ?: seedFilters())
        private set

    /** A Discover value's library and role, in saved state like the filters. */
    var facetScope by mutableStateOf(restore(FACET_SCOPE) ?: FacetScope((source as? GridSource.Facet)?.libraryId))
        private set

    /** Null until the stored options are read: the grid waits for its column count. */
    val options: StateFlow<GridOptions?> = settings.gridOptions.stateIn(viewModelScope, SharingStarted.Eagerly, null)

    /** From the first page; null until it has answered. */
    var total by mutableStateOf<Int?>(null)
        private set

    /** A library's name; null for All libraries and until it is known. */
    var libraryName by mutableStateOf<String?>(null)
        private set

    /** A series' contents, with what the outbox holds for them; null while they load, and for the paged sources. */
    var list by mutableStateOf<List<Content>?>(null)
        private set

    // A series' contents as fetched, and their effective readings.
    private var fetched: List<Content>? = null
    private var readings = emptyMap<String, EffectiveReading>()
    private var readingsIn = false

    /** The account generation this grid's memo and hint calls are for. */
    private val generation = content.owner()
    private val shownIds = MutableStateFlow(emptySet<String>())

    var listError by mutableStateOf<UiText?>(null)
        private set

    /** A series' contents from what is stored, the server being out of reach: unfiltered, as last fetched (P2 §10). */
    var stored by mutableStateOf(false)
        private set
    private val storedFlow = MutableStateFlow(false)

    /** Of a series' contents, those that can't be opened now: offline or [stored], and not downloaded. They are dimmed. */
    var unreadable by mutableStateOf(emptySet<String>())
        private set

    var listRefreshing by mutableStateOf(false)
        private set

    private var listJob: Job? = null

    private var paging: PagingSource<Int, PagedContent>? = null

    private var pagerScope: CoroutineScope? = null

    /** Not in saved state: after process death the grid isn't selecting. */
    var selection by mutableStateOf(Selection())
        private set

    private val account = session.state.value.account

    private val own = BatchFollower(viewModelScope, bulk.state) {
        dialog = null
        selection = Selection()
    }

    /** This grid's batch: the dialog's progress. */
    val ownBatch: StateFlow<BulkState?> = own.state

    /** The batch running, this grid's or another's: one at a time. */
    val running: StateFlow<BulkState?> = bulk.state

    var dialog by mutableStateOf<BulkDialog?>(null)
        private set

    /** The open add-to-lists sheet's opening (see `ListsSheetViewModel`). Not saved: select mode isn't either. */
    var listsOpening by mutableStateOf<Long?>(null)
        private set

    fun openLists() {
        listsOpening = Random.nextLong()
    }

    fun closeLists() {
        listsOpening = null
    }

    /** A selection's add landed for [opening]: select mode ends and the sheet closes only if it is still the current one. */
    fun listsAdded(opening: Long) {
        if (listsOpening != opening) return
        listsOpening = null
        selecting(false)
    }

    /**
     * A new Pager and a new flow per filter change, so the screen starts each over, loading. One
     * flow fed by successive pagers would keep the old filter's cards until the new first page.
     */
    var pages by mutableStateOf(pagesFor(filters))
        private set

    /**
     * A series' contents reload quietly: only the pull gesture shows their indicator. The paged
     * sources show it on every reload, as Paging reports a refresh.
     */
    val refresher = Refresher(viewModelScope, events, matches = source::follows, refresh = ::reload)

    init {
        loadLibraryName()
        if (source is GridSource.Contents) {
            // The effective reading of the listed volumes: newest server state with the unsent ops over it, fetched list or cached.
            viewModelScope.launch {
                shownIds.flatMapLatest { catalog.effective(it) }.collect {
                    readings = it
                    readingsIn = true
                    show()
                }
            }
        }
        if (source is GridSource.Contents) {
            viewModelScope.launch {
                combine(shownIds, storedFlow, ::Pair).flatMapLatest { (ids, stored) -> reader.unreadable(ids, stored) }.collect { unreadable = it }
            }
        }
    }

    private fun show() {
        shownIds.value = fetched.orEmpty().map { it.id }.toSet()
        // Until the readings have answered, what the last page showed stands in for them: display only, `fetched` stays what the server said.
        val shown = fetched?.map { row ->
            when {
                readingsIn -> row.withReading(readings[row.id])
                content.hasShownHint(generation, row.id) -> row.copy(userData = content.shownHint(generation, row.id))
                else -> row
            }
        }
        list = shown
        if (readingsIn && shown != null) content.rememberShown(generation, shown.associate { it.id to it.userData })
    }

    fun applyFilters(filters: GridFilters) {
        if (filters == this.filters) return
        this.filters = filters
        saved[FILTERS] = AppJson.encodeToString(filters)
        selection = selection.cleared()
        pages = pagesFor(filters)
    }

    fun applyFacetScope(scope: FacetScope) {
        if (scope == facetScope) return
        facetScope = scope
        saved[FACET_SCOPE] = AppJson.encodeToString(scope)
        selection = selection.cleared()
        pages = pagesFor(filters)
    }

    private fun pagesFor(filters: GridFilters): Flow<PagingData<PagedContent>> {
        if (source is GridSource.Contents) {
            // The list as an earlier visit or fetch left it shows until the fetch answers.
            fetched = content.knownList(generation, filters.toParams(source.base(filters, facetScope)).copy(count = false))
            total = fetched?.size
            show()
            loadList()
            return emptyFlow()
        }
        pagerScope?.cancel()
        val scope = CoroutineScope(viewModelScope.coroutineContext + Job(viewModelScope.coroutineContext[Job])).also { pagerScope = it }
        total = null
        val params = filters.toParams(source.base(filters, facetScope))
        // One per Pager: a refresh's new source learns from it how far the list was loaded.
        val reach = PagingReach()
        // The default prefetch distance (a page) fetched the second page at once. The source ignores the load size.
        val config = PagingConfig(
            ContentPagingSource.PAGE_SIZE,
            prefetchDistance = ContentPagingSource.PAGE_SIZE / 3,
            enablePlaceholders = false,
            // A long browse doesn't keep every page for as long as the grid is on the back stack; the ones scrolled away from load again.
            maxSize = ContentPagingSource.MAX_LOADED,
        )
        return Pager(config) {
            ContentPagingSource(content, params, reach, connectivity::settled) {
                total = it
                loadLibraryName()
            }.also { paging = it }
        }.flow.cachedIn(scope)
    }

    /** Pull-to-refresh and Retry. */
    fun refresh() {
        listRefreshing = source is GridSource.Contents && list != null
        reload()
    }

    /** Reloads the list as far as it was scrolled. Offline the paged grids show the offline state, and wait for [CatalogChange.OnlineChanged]. */
    private fun reload() {
        if (source is GridSource.Contents) loadList() else if (connectivity.online.value) paging?.invalidate()
    }

    /** Every volume at once, as the reader loads its siblings. A failed reload keeps the cards. Offline, or unreachable, what is stored. */
    private fun loadList() {
        listJob?.cancel()
        val seriesId = (source as GridSource.Contents).seriesId
        val params = filters.toParams(source.base(filters, facetScope)).copy(count = false)
        listJob = viewModelScope.launch {
            connectivity.settled()
            val online = connectivity.online.value
            val result = if (online) attemptResult { content.list(params).data } else Result.failure(IOException())
            val failure = result.exceptionOrNull()
            var readFailure: Throwable? = null
            val rows = result.getOrNull() ?: if (failure!!.isUnreachable()) {
                // A failed read of the store is the list's error, like the server's.
                attemptResult { catalog.volumes(seriesId) }.also { readFailure = it.exceptionOrNull() }.getOrNull()
            } else {
                null
            }
            if (rows != null) {
                fetched = rows.also { total = it.size }
                stored = result.isFailure
                storedFlow.value = stored
                listError = null
                show()
            } else {
                listError = (readFailure ?: failure)?.toUiText()
            }
            listRefreshing = false
        }
    }

    /** Again with each first page until it is known: the grid may have opened while the server was away. */
    private fun loadLibraryName() {
        if (source !is GridSource.Library || libraryName != null) return
        viewModelScope.launch { attempt { libraryName = users.libraries().find { it.id == source.id }?.name } }
    }

    /** The toolbar's toggle: in with nothing selected, or out. */
    fun selecting(on: Boolean) {
        selection = if (on) Selection(active = true) else Selection()
    }

    /** A long press, or a tap in select mode. False when the selection is full. */
    fun toggle(item: Content, series: String? = null): Boolean {
        // As fetched: a series' contents show what the outbox holds, which the command must not be made against.
        val base = fetched?.find { it.id == item.id } ?: item
        selection = selection.toggle(base, series) ?: return false
        return true
    }

    var downloadCount by mutableStateOf<DownloadCount>(DownloadCount.Counting)
        private set
    private var counting: Job? = null

    fun openDialog(which: BulkDialog?) {
        dialog = which
        counting?.cancel()
        if (which == BulkDialog.Download) countDownload()
    }

    /** Expands the selection, also for Retry. Offered volumes follow the rows as they are now. */
    fun countDownload() {
        counting?.cancel()
        downloadCount = DownloadCount.Counting
        val items = selection.items.values.toList()
        // Nothing else in the count or the download may resolve the open account again.
        val owner = downloads.current.value
        counting = viewModelScope.launch {
            val plan = attemptResult {
                checkNotNull(owner) { "No downloads open" }
                downloadPlan(items, owner::presentIds, content::volumes)
            }
            downloadCount = plan.fold(
                { DownloadCount.Ready(it, owner!!, downloads.freeBytes(), downloads.waitsForWifi()) },
                { DownloadCount.Failed(it.toUiText()) },
            )
        }
    }

    /**
     * Queues what the dialog offered, for the owner it counted for, and leaves select mode. It runs on after the
     * screen is gone. The rows are reconciled as they are then, so [done] gets the number actually queued, or why
     * not. It isn't called when that owner is no longer the open account.
     */
    fun download(done: (count: Int, error: UiText?) -> Unit) {
        val ready = downloadCount as? DownloadCount.Ready ?: return
        val plan = ready.plan.takeIf { it.count > 0 && !it.tooMany } ?: return
        hide()
        appScope.launch {
            val result = attemptResult { downloads.queueBulk(ready.owner, plan.offered) }
            // The owner may have closed while this ran: nothing is said for it.
            if (!downloads.isCurrent(ready.owner)) return@launch
            result.fold(
                {
                    when {
                        it == null -> {}
                        it.tooMany -> done(0, UiText.Plural(R.plurals.bulk_download_too_many, it.count))
                        it.count == 0 -> done(0, UiText.Res(R.string.bulk_download_nothing))
                        else -> done(it.count, null)
                    }
                },
                { done(0, it.toUiText()) },
            )
        }
    }

    fun setStatus(status: String?, includeUnread: Boolean) =
        start(BulkWhat.Status(status), statusPlan(selection.items.values, status, includeUnread))

    fun clear() = start(BulkWhat.Clear, clearPlan(selection.items.values))

    private fun start(what: BulkWhat, commands: List<BulkCommand>) {
        val id = bulk.start(account ?: return, what, commands) ?: return
        own.follow(BulkState(id, what, commands.size))
    }

    /** The dialog closes, select mode ends, and the batch goes on; this grid stops following it. */
    fun hide() {
        own.forget()
        counting?.cancel()
        dialog = null
        selection = Selection()
    }

    fun updateOptions(change: (GridOptions) -> GridOptions) {
        viewModelScope.launch { settings.updateGridOptions(change) }
    }

    fun resetOptions() {
        viewModelScope.launch { settings.resetGridOptions() }
    }

    private fun seedFilters(): GridFilters {
        val sort = (source as? GridSource.Browse)?.sort ?: return GridFilters()
        return GridFilters(sort = sort, sortOrder = source.sortOrder ?: GridFilters.defaultOrder(sort))
    }

    private inline fun <reified T> restore(key: String): T? = try {
        saved.get<String>(key)?.let { AppJson.decodeFromString<T>(it) }
    } catch (_: SerializationException) {
        null
    }

    private companion object {
        const val FILTERS = "filters"
        const val FACET_SCOPE = "facet_scope"
        val BROWSE_SORTS = listOf(ContentSort.CONTINUE, ContentSort.RECENTLY_UPDATED, ContentSort.HISTORY)
        val LIBRARY_SORTS = listOf(
            ContentSort.TITLE,
            ContentSort.CREATED_AT,
            ContentSort.LAST_READ_AT,
            ContentSort.RATING,
            ContentSort.USER_RATING,
            ContentSort.RELEASE_DATE,
            ContentSort.UNREAD_COUNT,
        )
    }
}
