package me.tijlvdb.voltis.ui.content

import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.assisted.Assisted
import dagger.assisted.AssistedFactory
import dagger.assisted.AssistedInject
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.flow.stateIn
import androidx.compose.runtime.snapshotFlow
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.domain.catalog.opens
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.UnexpectedResponse
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.api.ContinueTarget
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.api.attemptResult
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.content.ContentListParams
import me.tijlvdb.voltis.data.content.ContentRepository
import me.tijlvdb.voltis.data.content.ContentSort
import me.tijlvdb.voltis.data.content.DownloadedCatalog
import me.tijlvdb.voltis.data.content.GridFilters
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.data.downloads.isDetail
import me.tijlvdb.voltis.data.net.isContentMissing
import me.tijlvdb.voltis.data.net.isUnreachable
import me.tijlvdb.voltis.data.sync.PendingSync
import me.tijlvdb.voltis.data.user.UserRepository
import me.tijlvdb.voltis.domain.catalog.completingAsks
import me.tijlvdb.voltis.domain.catalog.VolumeReading
import me.tijlvdb.voltis.domain.catalog.hasVolumes
import me.tijlvdb.voltis.domain.catalog.isReadable
import me.tijlvdb.voltis.domain.catalog.isSeries
import me.tijlvdb.voltis.domain.catalog.offlineContinue
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.net.settled
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.reading.ReadingCommands
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.ReadingSync
import me.tijlvdb.voltis.domain.reading.Shown
import me.tijlvdb.voltis.data.content.withReading
import me.tijlvdb.voltis.domain.reading.EffectiveReading
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.reading.titled
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.domain.sync.PendingUserData
import me.tijlvdb.voltis.domain.sync.UserDataItem
import me.tijlvdb.voltis.domain.sync.UserDataSync
import me.tijlvdb.voltis.domain.sync.withSync
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.ui.Refresher
import me.tijlvdb.voltis.ui.Reviewer
import me.tijlvdb.voltis.ui.notAppliedText
import me.tijlvdb.voltis.ui.UiText

/** What a page's first frame is made of, for one account generation: rows as the server said them, and display hints. */
class FirstFrame(
    val known: (id: String) -> Content?,
    /** A row from the store as it was fetched, downloaded or not. */
    val stored: suspend (id: String) -> Content?,
    /** A row from the store joins the memo, as detail when it is one. */
    val adopt: (Content) -> Unit,
    /** The item is gone or off limits: nothing kept of it may show it again. */
    val forget: (id: String) -> Unit,
    val hasHint: (id: String) -> Boolean,
    val hint: (id: String) -> UserData?,
    val remember: (id: String, shown: UserData?) -> Unit,
    /** Whether the series' downloads line last had rows, null when never seen. */
    val downloadsLine: (seriesId: String) -> Boolean? = { null },
    val rememberDownloadsLine: (seriesId: String, present: Boolean) -> Unit = { _, _ -> },
) {
    companion object {
        val None = FirstFrame({ null }, { null }, {}, {}, { false }, { null }, { _, _ -> })
    }
}

/** For the account open now: every call is for that generation, whatever has opened since. */
fun ContentRepository.firstFrame(catalog: DownloadedCatalog): FirstFrame {
    val owner = owner()
    return FirstFrame(
        known = { known(owner, it) },
        // The captured store's own database, and the generation checked before and after the read: a row of an account that has gone is dropped.
        stored = { id ->
            if (owner == null || owner() !== owner) {
                null
            } else {
                catalog.fetchedRow(owner.db, id).takeIf { owner() === owner }
            }
        },
        adopt = { adopt(owner, it) },
        forget = { forget(owner, it) },
        hasHint = { hasShownHint(owner, it) },
        hint = { shownHint(owner, it) },
        remember = { id, shown -> rememberShown(owner, mapOf(id to shown)) },
        downloadsLine = { downloadsLine(owner, it) },
        rememberDownloadsLine = { id, present -> rememberDownloadsLine(owner, id, present) },
    )
}

@Serializable
sealed interface ContentDialog {
    /** Clear status and position. Read again opens [thenRead] once cleared. */
    @Serializable
    data class Clear(val thenRead: String? = null) : ContentDialog

    @Serializable
    data object Complete : ContentDialog

    @Serializable
    data object UpdateProgress : ContentDialog

    /** The add-to-list sheet; each time it opens is a new [opening]. */
    @Serializable
    data class Lists(val opening: Long) : ContentDialog
}

sealed interface ContentEffect {
    /** A snackbar. */
    data class Message(val text: UiText) : ContentEffect

    data class Read(val contentId: String) : ContentEffect
}

/**
 * One content page: a series, a volume or a book, with its reading state and the writes that change
 * it. Reading writes go through the outbox, and what is still unsent shows over what was fetched.
 */
@OptIn(ExperimentalCoroutinesApi::class)
@HiltViewModel(assistedFactory = ContentViewModel.Factory::class)
class ContentViewModel internal constructor(
    private val id: String,
    private val saved: SavedStateHandle,
    private val repository: ContentRepository,
    private val reading: ReadingCommands,
    private val sync: ReadingSync,
    /** Star and rating: stored at once, sent later (P4 §8). */
    private val userData: UserDataSync,
    /** The account the page was opened for: the stack is cleared when it changes. */
    private val account: String?,
    /** A series' cached volumes in order, when it has any. */
    private val cachedVolumes: suspend (seriesId: String) -> List<Content>?,
    /** A downloaded item or a series with downloads, as stored: the page offline. */
    private val cachedContent: suspend (id: String) -> Content?,
    /** Whether the item, or one of a series' volumes, has a copy on this device. */
    hasCopy: Flow<Boolean>,
    private val connectivity: Connectivity,
    users: UserRepository,
    events: CatalogEvents,
    /** A cached series' volumes with their effective readings, as they change; null while its list or a reading isn't known. */
    private val cachedSeries: (seriesId: String) -> Flow<List<VolumeReading>?> = { flowOf(null) },
    /** The effective readings (newest server state, unsent ops over it) of the item, its series and the listed volumes, as they change. */
    private val effective: (ids: Set<String>) -> Flow<Map<String, EffectiveReading>> = { flowOf(emptyMap()) },
    /** What the page's first frame is made of, all for the generation of the account it was opened for. */
    private val first: FirstFrame = FirstFrame.None,
    /** A series' download rows, whether it has automatic downloads, and an item's copy: the page's rows, read ahead so they are there when it scrolls back. */
    seriesRows: Flow<List<DownloadEntity>> = flowOf(emptyList()),
    autoDownloads: Flow<Boolean> = flowOf(false),
    copyOf: (id: String) -> Flow<Boolean> = { flowOf(false) },
    private val log: (String, Throwable) -> Unit = { message, e -> Log.w("ContentViewModel", message, e) },
) : ViewModel() {
    @AssistedInject
    constructor(
        @Assisted id: String,
        saved: SavedStateHandle,
        repository: ContentRepository,
        reading: ReadingCommands,
        sync: ReadingSync,
        userData: PendingSync,
        session: SessionStore,
        catalog: DownloadedCatalog,
        downloads: DownloadRepository,
        connectivity: Connectivity,
        users: UserRepository,
        events: CatalogEvents,
    ) : this(
        id, saved, repository, reading, sync, userData, session.state.value.account, catalog::volumes, catalog::content,
        combine(downloads.download(id), downloads.series(id)) { item, volumes -> item?.copyId != null || volumes.any { it.copyId != null } },
        connectivity, users, events,
        cachedSeries = catalog::series, effective = catalog::effective,
        first = repository.firstFrame(catalog),
        seriesRows = downloads.series(id), autoDownloads = downloads.policies.map { id in it },
        copyOf = { downloads.download(it).map { row -> row?.copyId != null } },
    )

    @AssistedFactory
    interface Factory {
        fun create(id: String): ContentViewModel
    }

    /** For the reading speeds behind the length's time estimate. */
    val me = users.me

    // What was known before the fetch: shown until it lands, never the base of a command.
    private var seed: Content? = first.known(id)
    private var gone = false
    private var seedParent: Content? = seed?.parentId?.let(first.known)


    // What the last page showed of the item's reading: the first frame only, until both flows below have answered.
    private val hint: UserData? = if (first.hasHint(id)) first.hint(id) else null
    private val hinted = first.hasHint(id)
    private var readingsIn = false
    private var pendingIn = account == null

    /** As fetched, with what the outbox and the pending star and rating hold for it applied; before that, what was already known. */
    var content by mutableStateOf(if (hinted) seed?.copy(userData = hint) else seed)
        private set

    /** The series of a volume, once loaded. */
    var parent by mutableStateOf(seedParent)
        private set

    /** A command can be made: the row the server returned is held. Until then the page shows what was known, and star and rating wait. */
    var ready by mutableStateOf(false)
        private set

    /** The parent's attempt is still open: its row is held in the layout. */
    private var parentSettled by mutableStateOf(false)
    val parentPending get() = content?.parentId != null && parent == null && !parentSettled

    /** The series' download rows, for its "N of M downloaded" line. */
    val seriesDownloads: StateFlow<List<DownloadEntity>?> = seriesRows
        .onEach { first.rememberDownloadsLine(id, it.isNotEmpty()) }
        .stateIn<List<DownloadEntity>?>(viewModelScope, SharingStarted.Eagerly, null)

    /** The line had rows when the series was last shown: its place is held until they are known. */
    val holdsDownloadsLine = first.downloadsLine(id) == true



    /** The series has automatic downloads. */
    val autoDownload: StateFlow<Boolean> = autoDownloads.stateIn(viewModelScope, SharingStarted.Eagerly, false)

    /** Why the page couldn't be loaded. A failed reload keeps what it showed, without an error when the server just wasn't reached. */
    var error by mutableStateOf<UiText?>(null)
        private set

    // What the server returned: a command is made on it, and the lane is seeded from the stored snapshot.
    private var fetched: Content? = null
    private var fetchedParent: Content? = null
    private var fetchedVolumes: List<Content>? = null

    /** The item's pending row, wish or landed; the row flow alone feeds it. Null while no owner is bound. */
    private var row: PendingUserData? = null

    /** The `landedSeq` read before the last load that installed the main row: a landed answer newer than it shows over that load. */
    private var loadedAt = 0L

    /** The lanes' projections of the item and its series: review bookkeeping only, never shown as reading. */
    private var shown = emptyMap<String, Shown>()

    /** What the phone knows of the reading of the item, its series and the listed volumes. */
    private var readings = emptyMap<String, EffectiveReading>()
    private val shownIds = MutableStateFlow(setOf(id))

    var refreshing by mutableStateOf(false)
        private set

    /** Shown from what is stored, the server being out of reach (P2 §10): what needs the server is disabled. */
    var cached by mutableStateOf(false)
        private set

    /** Out of reach, and nothing stored for this page: the offline state instead. */
    var unavailable by mutableStateOf(false)
        private set

    /** The item changed on another device while this one held unsent reading of it: "Changed on another device" with Review. */
    var needsReview by mutableStateOf(false)
        private set

    val reviewer = Reviewer(viewModelScope, sync, account) { _effects.trySend(ContentEffect.Message(it)) }

    /** "Changed on another device" turned up while the page was open, rather than with it: it is announced. */
    var reviewAppeared by mutableStateOf(false)
        private set
    private var shownSeen = false

    /** What the continue button opens; null until loaded, and for books, which aren't asked. */
    var next by mutableStateOf<ContinueTarget?>(null)
        private set

    /** Continue's target has a copy on this device. */
    val nextCopy: StateFlow<Boolean> = snapshotFlow { next?.opens }.distinctUntilChanged()
        .flatMapLatest { target -> target?.let(copyOf) ?: flowOf(false) }
        .stateIn(viewModelScope, SharingStarted.Eagerly, false)

    /** The continue target couldn't be loaded or, with [next] there, Read again couldn't find the first volume. */
    var nextFailed by mutableStateOf(false)
        private set

    /** A write is on its way, or the page hasn't shown its outcome yet: the controls wait. */
    var busy by mutableStateOf(seed != null)
        private set

    /** In saved state, with Read again's destination: an open dialog comes back after process death. */
    var dialog by mutableStateOf(restoreDialog())
        private set

    /** Why the open dialog's action failed. */
    var dialogError by mutableStateOf<UiText?>(null)
        private set

    /** The series' volumes in order, for the Update progress dialog; loaded each time it opens. */
    var volumes by mutableStateOf<List<Content>?>(null)
        private set

    var volumesError by mutableStateOf<UiText?>(null)
        private set

    private val _effects = Channel<ContentEffect>(Channel.BUFFERED)
    val effects = _effects.receiveAsFlow()

    // Every request belongs to one of these jobs. Newer state cancels the job, and a response is
    // applied only by a job that is still active (`fresh`), so a late answer never overwrites it.
    private var job: Job? = null
    private var nextJob: Job? = null
    private var lookup: Job? = null
    private var volumesJob: Job? = null

    /** The request of a write is out; [busy] lasts until the reload after it. */
    private var writing = false

    /** Follows changes of the item, of its series, and of a series' volumes. A write of this page reloads it itself. */
    val refresher = Refresher(
        viewModelScope,
        events,
        matches = { change ->
            !writing && when (change) {
                is CatalogChange.ReadingChanged -> id == change.contentId || id == change.parentId || content?.parentId == change.parentId
                is CatalogChange.UserDataChanged -> id == change.contentId
                // The server came or went: every screen reloads once (Refresher).
                is CatalogChange.OnlineChanged -> true
                is CatalogChange.PositionSaved, CatalogChange.PreferencesChanged -> false
            }
        },
        refresh = ::load,
    )

    init {
        // A page shown from storage whose copy went: loaded again, it is the offline state.
        viewModelScope.launch { hasCopy.distinctUntilChanged().collect { if (!it && cached) load() } }
        viewModelScope.launch {
            shownIds.flatMapLatest { effective(it) }.collect {
                readings = it
                readingsIn = true
                publish()
            }
        }
        if (account != null) {
            viewModelScope.launch {
                userData.pending(account, id).collect {
                    row = it
                    pendingIn = true
                    publish()
                }
            }
            viewModelScope.launch {
                shownIds.flatMapLatest { sync.shown(it, account) }.collect {
                    if (shownSeen && !needsReview && it[id]?.needsReview == true) reviewAppeared = true
                    shownSeen = true
                    shown = it
                    publish()
                }
            }
        }
        if (seed == null || seed?.isDetail == false) {
            // Nothing in memory, or only a list row: what is stored shows meanwhile, as soon as the database answers.
            viewModelScope.launch {
                val item = attemptResult { first.stored(id) }.getOrNull()?.takeIf { it.isDetail } ?: return@launch
                if (fetched != null || gone) return@launch
                first.adopt(item)
                seed = item
                seedParent = item.parentId?.let { attemptResult { first.known(it) ?: first.stored(it) }.getOrNull() }
                if (job?.isActive == true && !writing) busy = true
                publish()
            }
        }
        load()
    }

    /**
     * Publishes what was fetched with the effective reading of each item over it (the newest server state with the
     * unsent ops applied). A lane's own copy of the server's state may be older, so it only says whether a review is due.
     */
    private fun publish() {
        fun Content.withEffective() = withReading(readings[id])
        val item = fetched ?: seed
        shownIds.value = setOfNotNull(id, item?.parentId) + fetchedVolumes.orEmpty().map { it.id }
        needsReview = this.shown[id]?.needsReview == true
        ready = fetched != null
        content = item?.withSync(row, loadedAt)?.withEffective()
        // What shows is what the next visit starts from: its first frame has the reading and the unsent changes already.
        // Display only: what the next page shows first, until its own flows answer. Never a base of anything.
        if (fetched != null && readingsIn && pendingIn) first.remember(id, content?.userData)
        // Also once the fetch has landed: `fetched` stays what the server said, and only what shows waits for the flows.
        if (hinted && !(readingsIn && pendingIn)) content = content?.copy(userData = hint)
        parent = (fetchedParent ?: seedParent)?.withEffective()
        volumes = fetchedVolumes?.map { it.withEffective() }
    }

    fun refresh() {
        refreshing = true
        load()
    }

    private fun load() {
        job?.cancel()
        // The continue target is asked again: a retry or a lookup for the old one is obsolete.
        nextJob?.cancel()
        lookup?.cancel()
        job = viewModelScope.launch {
            connectivity.settled()
            // Before the main row is read, from the server or from storage: a landing committed after this stamp shows over the load.
            val stamp = landedStamp()
            val failure = if (connectivity.online.value) {
                attemptResult {
                    val loaded = fresh { repository.get(id) }
                    fetched = loaded
                    stamp?.let { loadedAt = it }
                    cached = false
                    unavailable = false
                    publish()
                    coroutineScope {
                        // The page works without its parent link.
                        launch {
                            attempt {
                                try {
                                    fetchedParent = loaded.parentId?.let { fresh { repository.get(it) } }
                                    publish()
                                } finally {
                                    parentSettled = true
                                }
                            }
                        }
                        launch { loadNext(loaded) }
                    }
                }.exceptionOrNull()
            } else {
                null
            }
            // Offline, or a first load that didn't reach the server: what is stored, else the offline state.
            if (!connectivity.online.value || (failure?.isUnreachable() == true && (fetched == null || cached))) {
                // A failed read of the store is the page's error, unless something is shown already.
                error = attemptResult { loadCached(stamp) }.exceptionOrNull()?.takeIf { fetched == null }?.toUiText()
                // Online, and the server's answer wasn't readable, with nothing stored: an error to retry, not the offline state.
                if (unavailable && failure is UnexpectedResponse && connectivity.online.value) {
                    unavailable = false
                    error = failure.toUiText()
                }
            } else {
                // The server answered: the page shows that error, not the offline state of an earlier load.
                unavailable = false
                if (failure?.isContentMissing() == true) invalidate()
                // A reload that didn't reach the server keeps the page as it is, with what is unsent over it.
                error = failure?.takeUnless { fetched != null && it.isUnreachable() }?.toUiText()
            }
            refreshing = false
            if (!writing) busy = false
        }
    }

    /** The server says the item isn't there, or isn't for this account: it is no longer shown from memory, and not offered again. */
    private fun invalidate() {
        gone = true
        first.forget(id)
        seed = null
        seedParent = null
        fetched = null
        fetchedParent = null
        fetchedVolumes = null
        nextJob?.cancel()
        next = null
        nextFailed = false
        cached = false
        parentSettled = true
        publish()
    }

    /** The landed sequence now; null without an owner of the page's account (the load goes on and keeps its stamp). */
    private suspend fun landedStamp(): Long? {
        val account = account ?: return null
        return try {
            userData.landedSeq(account)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // No owner, or a database closed by a sign-out: the load goes on and keeps its stamp.
            if (e !is SyncUnavailable) log("Couldn't read the landed sequence", e)
            null
        }
    }

    /** The stored item and its series, without a continue target. */
    private suspend fun loadCached(stamp: Long?) {
        val item = fresh { cachedContent(id) }
        unavailable = item == null
        if (item == null) return
        fetched = item
        stamp?.let { loadedAt = it }
        fetchedParent = null
        next = null
        nextFailed = false
        cached = true
        // A series' continue target is found from what is stored, as it changes; an old server's or an unknown one's is none.
        if (item.isSeries) {
            nextJob = viewModelScope.launch { cachedSeries(id).collect { volumes -> next = volumes?.let { offlineContinue(id, it) } } }
        }
        // The item shows without its series, which is optional: a failed read of it only leaves the link out.
        publish()
        item.parentId?.let { parentId ->
            val parent = attemptResult { fresh { cachedContent(parentId) } }
            parent.exceptionOrNull()?.let { log("Couldn't read the stored series", it) }
            fetchedParent = parent.getOrNull()
            parentSettled = true
            publish()
        }
    }

    private suspend fun loadNext(content: Content) {
        if (!content.isReadable) return
        val failed = attempt { next = fresh { repository.continueTarget(id) } } != null
        nextFailed = failed && next == null
    }

    /** [block]'s outcome, unless this job was cancelled while it ran: a superseded answer or failure is dropped. */
    private suspend fun <T> fresh(block: suspend () -> T): T = try {
        block()
    } finally {
        currentCoroutineContext().ensureActive()
    }

    /** Asks about the change made elsewhere with the reader's dialog. */
    fun review() = reviewer.start(id, content?.title.orEmpty())

    /** The continue button's Retry: with a target, what failed was Read again's lookup. */
    fun retryNext() {
        if (next != null) return readAgain()
        val content = content ?: return
        nextJob?.cancel()
        nextJob = viewModelScope.launch { loadNext(content) }
    }

    /** Completing a series with unread volumes asks whether they are read too. A null [status] keeps the position. */
    fun setStatus(status: String?) {
        if (content?.completingAsks(status) == true) return show(ContentDialog.Complete)
        write(statusMessage(status)) { item -> reading.setStatus(item, status) }
    }

    fun setStarred(starred: Boolean) {
        submit(UiText.Res(if (starred) R.string.msg_starred else R.string.msg_unstarred), starred = starred)
    }

    /** 1 to 5, or null to clear it. */
    fun rate(rating: Int?) {
        if (rating == content?.userData?.rating) return
        submit(rating?.let { UiText.Plural(R.plurals.msg_rated, it) } ?: UiText.Res(R.string.msg_rating_cleared), rating = rating?.let { JsonPrimitive(it) } ?: JsonNull)
    }

    /**
     * Stored at once, online too, and sent later: the page shows it from the item's row, so nothing here
     * is busy and nothing reloads (only the landing's `UserDataChanged` does). A refusal later arrives as a notice.
     */
    private fun submit(done: UiText, starred: Boolean? = null, rating: JsonElement? = null) {
        val item = fetched ?: return
        val account = account ?: return
        val named = UserDataItem(item.id, item.libraryId, item.uri, titled(fetchedParent?.title, item.title))
        viewModelScope.launch {
            val failure = attemptResult { userData.setUserData(account, named, starred, rating) }.exceptionOrNull()
            _effects.send(ContentEffect.Message(failure?.let(::failureText) ?: done))
        }
    }

    fun show(dialog: ContentDialog) {
        // Read again's lookup would open its own dialog over this one.
        lookup?.cancel()
        open(dialog)
    }

    private fun open(dialog: ContentDialog) {
        if (busy) return
        dialogError = null
        // The picker lists the volumes as they are now, not what an earlier opening asked for.
        volumesJob?.cancel()
        fetchedVolumes = null
        volumes = null
        volumesError = null
        put(dialog)
    }

    fun dismiss() {
        if (!busy) put(null)
    }

    private fun put(dialog: ContentDialog?) {
        this.dialog = dialog
        saved[DIALOG] = dialog?.let { AppJson.encodeToString(it) }
    }

    private fun restoreDialog(): ContentDialog? = try {
        saved.get<String>(DIALOG)?.let { AppJson.decodeFromString(it) }
    } catch (_: SerializationException) {
        null
    }

    /** The Clear dialog's confirmation. */
    fun clear() {
        val thenRead = (dialog as? ContentDialog.Clear)?.thenRead
        write(UiText.Res(R.string.msg_cleared), thenRead) { item -> reading.clear(item) }
    }

    fun markSeriesCompleted(includeUnread: Boolean) {
        write(UiText.Res(R.string.msg_series_completed)) { item -> reading.markSeriesCompleted(item, includeUnread) }
    }

    /** Completes the volumes up to [untilId]. Null marks all: through the last volume, looked up first. */
    fun markThrough(untilId: String?) {
        val series = content ?: return
        if (untilId == null && (series.childrenCount ?: 0) == 0) {
            dialogError = UiText.Res(if (series.hasVolumes) R.string.update_no_volumes else R.string.update_no_chapters)
            return
        }
        write(UiText.Res(R.string.msg_progress_updated)) { item ->
            val last = ContentListParams(parentId = id, sort = ContentSort.ORDER, sortOrder = GridFilters.DESC)
            val until = untilId ?: orCached({ repository.firstId(last) }) { cachedVolumes(id)?.lastOrNull()?.id }
            reading.markThrough(item, checkNotNull(until))
        }
    }

    /** For the Update progress dialog's picker. */
    fun loadVolumes() {
        if (volumes != null || volumesJob?.isActive == true) return
        volumesJob = viewModelScope.launch {
            volumesError = attempt {
                fetchedVolumes = fresh { orCached({ repository.volumes(id) }) { cachedVolumes(id) } }
                publish()
            }
        }
    }

    /** Offline, what is cached first; online, the server, then the cache when it can't be reached: the dialog works with the server cut. */
    private suspend fun <T> orCached(remote: suspend () -> T, cached: suspend () -> T?): T {
        if (!connectivity.online.value) cached()?.let { return it }
        return try {
            remote()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            if (!e.isUnreachable()) throw e
            connectivity.unreachable()
            cached() ?: throw e
        }
    }

    /** Starts over: the item itself, or a series' first volume, found before anything is cleared. */
    fun readAgain() {
        if (busy || lookup?.isActive == true) return
        val series = next?.seriesId == id
        lookup = viewModelScope.launch {
            nextFailed = false
            var first: String? = id
            if (series) {
                val params = ContentListParams(parentId = id, valid = true, sort = ContentSort.ORDER, sortOrder = GridFilters.ASC)
                val failed = attempt { first = fresh { repository.firstId(params) } } != null
                if (failed || first == null) {
                    nextFailed = true
                    return@launch
                }
            }
            open(ContentDialog.Clear(thenRead = first))
        }
    }

    /**
     * Sends one write, then reloads the page; [busy] lasts until that reload has landed, so the
     * controls never show the old value as changeable. A success closes the dialog and says
     * [done], also when the write is only queued: it shows at once and is sent later. A failure says
     * so briefly, in the dialog when one is open: the server's message, or "Couldn't update".
     */
    private fun write(done: UiText, thenRead: String? = null, block: suspend (item: Content) -> Any?) {
        // As fetched: the lane is seeded with the server's state, not with what is unsent.
        val item = fetched ?: return
        if (busy) return
        busy = true
        writing = true
        dialogError = null
        viewModelScope.launch {
            val result = attemptResult { block(item) }
            val failure = result.exceptionOrNull()?.let(::failureText)
            writing = false
            load()
            if (failure == null) {
                put(null)
                // Kept in memory only: it shows, and goes when space is back. Not "done", and no Retry, as it is still queued.
                val unsaved = result.getOrNull() == CommandOutcome.UNSAVED
                _effects.send(ContentEffect.Message(if (unsaved) UiText.Res(R.string.error_not_saved_storage_full) else done))
                if (!unsaved) thenRead?.let { _effects.send(ContentEffect.Read(it)) }
            } else if (dialog != null) {
                dialogError = failure
            } else {
                _effects.send(ContentEffect.Message(failure))
            }
        }
    }

    private fun failureText(e: Throwable): UiText = when (e) {
        is ReadingFailure.ChangedElsewhere -> notAppliedText(e.command)
        is StorageFullException -> e.toUiText()
        else -> e.toUiText().takeIf { it is UiText.Raw || e is SyncUnavailable } ?: UiText.Res(R.string.msg_update_failed)
    }

    private fun statusMessage(status: String?) = UiText.Res(
        when (status) {
            ReadingStatus.READING -> R.string.msg_marked_reading
            ReadingStatus.COMPLETED -> R.string.msg_marked_completed
            ReadingStatus.ON_HOLD -> R.string.msg_marked_on_hold
            ReadingStatus.DROPPED -> R.string.msg_marked_dropped
            ReadingStatus.PLAN_TO_READ -> R.string.msg_marked_plan_to_read
            else -> R.string.msg_status_cleared
        },
    )

    private companion object {
        const val DIALOG = "dialog"
    }
}
