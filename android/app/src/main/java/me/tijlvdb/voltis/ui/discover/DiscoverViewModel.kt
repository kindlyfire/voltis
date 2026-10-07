package me.tijlvdb.voltis.ui.discover

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.Facet
import me.tijlvdb.voltis.data.api.Library
import me.tijlvdb.voltis.data.api.attemptResult
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.facets.FacetRepository
import me.tijlvdb.voltis.data.user.UserRepository
import me.tijlvdb.voltis.domain.catalog.FacetKind
import me.tijlvdb.voltis.domain.catalog.FacetSort
import me.tijlvdb.voltis.ui.Refresher
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.net.settled
import me.tijlvdb.voltis.ui.UiText

/** What Discover lists: the web's URL query of `DiscoverPage.vue`, less the page. */
@Serializable
data class DiscoverQuery(
    val kind: String = FacetKind.GENRES,
    val q: String = "",
    val sort: String = FacetSort.COUNT,
    val order: String = FacetSort.defaultOrder(FacetSort.COUNT),
    val library: String? = null,
)

/**
 * The port of `DiscoverPage.vue`, with pages loaded as the list scrolls. Any change of the query
 * cancels the request in flight and starts over at the first page; the filter carries across kinds.
 */
@HiltViewModel
class DiscoverViewModel @Inject constructor(
    private val saved: SavedStateHandle,
    private val facets: FacetRepository,
    private val users: UserRepository,
    private val connectivity: Connectivity,
    events: CatalogEvents,
) : ViewModel() {
    var query by mutableStateOf(restore() ?: DiscoverQuery())
        private set

    /** Null until the first page of this query has answered. */
    var rows by mutableStateOf<List<Facet>?>(null)
        private set

    var total by mutableStateOf<Int?>(null)
        private set

    /** The first page or a reload failed; the rows a failed reload had stay. */
    var error by mutableStateOf<UiText?>(null)
        private set

    var appending by mutableStateOf(false)
        private set

    var appendError by mutableStateOf<UiText?>(null)
        private set

    /** Several pages in a row added nothing new: the next waits for Load more. */
    var stalled by mutableStateOf(false)
        private set

    /** The library select's options; null until they have loaded. */
    var libraries by mutableStateOf<List<Library>?>(null)
        private set

    /** The options failed to load; the values show regardless. */
    var librariesError by mutableStateOf<UiText?>(null)
        private set

    private var librariesJob: Job? = null

    /** How many rows the server has given for this query: pages can overlap, so not the rows shown. */
    private var offset = 0

    /** The server gave a short page or reached its total: no more pages. */
    private var ended = false

    /** The filter as typed, before the debounce. */
    private var typed = query.q

    private var filterJob: Job? = null

    private var job: Job? = null

    /** Discover's values change with scans, which this app doesn't hear of: only the 30-second rule, and the server coming or going. */
    val refresher = Refresher(viewModelScope, events, matches = { it is CatalogChange.OnlineChanged }, refresh = ::reload)

    init {
        reload()
    }

    /** Apart from the values, so neither waits for nor hides the other. */
    fun loadLibraries() {
        if (libraries != null || librariesJob?.isActive == true) return
        librariesError = null
        librariesJob = viewModelScope.launch {
            connectivity.settled()
            attemptResult { users.libraries() }.onSuccess { libraries = it }.onFailure { librariesError = it.toUiText() }
        }
    }

    fun setKind(kind: String) = update { it.copy(kind = kind) }

    fun setLibrary(id: String?) = update { it.copy(library = id) }

    fun toggleSort(tapped: String) = update {
        val (sort, order) = FacetSort.toggle(it.sort, it.order, tapped)
        it.copy(sort = sort, order = order)
    }

    /** Applied once typing pauses. */
    fun setFilter(text: String) {
        val q = text.trim().take(MAX_FILTER)
        if (q == typed) return
        typed = q
        filterJob?.cancel()
        filterJob = viewModelScope.launch {
            delay(DEBOUNCE_MS)
            update { it.copy(q = q) }
        }
    }

    private fun update(change: (DiscoverQuery) -> DiscoverQuery) {
        val next = change(query)
        if (next == query) return
        query = next
        saved[QUERY] = AppJson.encodeToString(next)
        rows = null
        total = null
        offset = 0
        reload()
    }

    /**
     * The first page, or as far as the list was loaded (up to the server's limit, past which a
     * deep scroll starts over), so a refresh keeps the list's length. Also Retry.
     */
    fun reload() {
        loadLibraries()
        job?.cancel()
        val q = query
        val limit = offset.coerceIn(PAGE_SIZE, MAX_LIMIT)
        error = null
        appendError = null
        appending = false
        stalled = false
        job = viewModelScope.launch {
            connectivity.settled()
            val page = attemptResult { facets.list(q.kind, q.q, q.sort, q.order, q.library, 0, limit) }
            // A superseded query's answer changes nothing.
            ensureActive()
            page.onSuccess {
                rows = it.data.distinctBy(Facet::key)
                seen.clear()
                it.data.mapTo(seen, Facet::key)
                total = it.total
                offset = it.data.size
                ended = it.data.size < limit || offset >= it.total
            }.onFailure { error = it.toUiText() }
        }
    }

    /** The keys of [rows], kept beside them so a page's dedup doesn't hash everything shown. */
    private val seen = HashSet<String>()

    /**
     * The next page, until the server's pages run out, one at a time. A value already shown is
     * left out: a scan or a count change between pages can move one across a page boundary. A
     * page that adds nothing is followed by the next at once, as the screen's scroll trigger
     * only fires again when the rows change; after [MAX_EMPTY_PAGES] of them it stops at [stalled].
     */
    fun loadMore() {
        if (rows == null || job?.isActive == true || appendError != null || ended) return
        val q = query
        appending = true
        stalled = false
        job = viewModelScope.launch {
            var empty = 0
            var added: Int?
            do {
                val shown = rows.orEmpty()
                val from = offset
                val page = attemptResult { facets.list(q.kind, q.q, q.sort, q.order, q.library, from, PAGE_SIZE) }
                ensureActive()
                added = page.fold(
                    onSuccess = {
                        val fresh = it.data.filter { row -> seen.add(row.key) }
                        rows = shown + fresh
                        total = it.total
                        offset = from + it.data.size
                        ended = it.data.size < PAGE_SIZE || offset >= it.total
                        fresh.size
                    },
                    onFailure = {
                        appendError = it.toUiText()
                        null
                    },
                )
                if (added == 0) empty++
            } while (added == 0 && !ended && empty < MAX_EMPTY_PAGES)
            stalled = added == 0 && !ended
            appending = false
        }
    }

    fun retryMore() {
        appendError = null
        loadMore()
    }

    private fun restore(): DiscoverQuery? = try {
        saved.get<String>(QUERY)?.let { AppJson.decodeFromString<DiscoverQuery>(it) }
    } catch (_: SerializationException) {
        null
    }

    private companion object {
        const val QUERY = "query"
        const val DEBOUNCE_MS = 300L
        const val MAX_FILTER = 200
        const val PAGE_SIZE = 100
        const val MAX_LIMIT = 500
        const val MAX_EMPTY_PAGES = 5
    }
}
