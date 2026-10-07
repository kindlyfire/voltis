package me.tijlvdb.voltis.ui.reader

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.assisted.Assisted
import dagger.assisted.AssistedFactory
import dagger.assisted.AssistedInject
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlin.math.abs
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.firstOrNull
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.job
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withPermit
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.api.attemptResult
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.data.net.isUnreachable
import me.tijlvdb.voltis.data.settings.ReaderSettingsStore
import me.tijlvdb.voltis.domain.comic.OpenedComic
import me.tijlvdb.voltis.domain.comic.ReaderData
import me.tijlvdb.voltis.domain.comic.ReaderMode
import me.tijlvdb.voltis.domain.comic.ReaderSettings
import me.tijlvdb.voltis.domain.comic.ReadingDirection
import me.tijlvdb.voltis.domain.comic.SeriesSettings
import me.tijlvdb.voltis.domain.comic.Siblings
import me.tijlvdb.voltis.domain.comic.controlsFlipped
import me.tijlvdb.voltis.domain.comic.detectDirection
import me.tijlvdb.voltis.domain.comic.detectMode
import me.tijlvdb.voltis.domain.comic.getPagesInPreloadOrder
import me.tijlvdb.voltis.domain.comic.loadSiblings
import me.tijlvdb.voltis.domain.comic.pageFor
import me.tijlvdb.voltis.domain.comic.preloadQuietly
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.reading.ComicAdapter
import me.tijlvdb.voltis.domain.reading.EmptyProgress
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.ReadingSession
import me.tijlvdb.voltis.domain.reading.ReadingSync
import me.tijlvdb.voltis.domain.reading.finishProgress
import me.tijlvdb.voltis.domain.reading.position
import me.tijlvdb.voltis.ui.UiText

private const val PRELOAD_COUNT = 10
private const val PRELOAD_CONCURRENCY = 3

/**
 * One comic in the reader. Its session of the reading sync places it where it opens; settled pages
 * are reading ([ReadingSession.moved]), the slider and layout changes are placements, and the end
 * is a finish.
 */
@HiltViewModel(assistedFactory = ReaderViewModel.Factory::class)
class ReaderViewModel @AssistedInject constructor(
    @Assisted private val contentId: String,
    private val data: ReaderData,
    private val settingsStore: ReaderSettingsStore,
    private val readingSync: ReadingSync,
    private val connectivity: Connectivity,
) : ViewModel() {
    @AssistedFactory
    interface Factory {
        fun create(contentId: String): ReaderViewModel
    }

    /** Null until the comic is loaded and placed on its page; input is ignored before that. */
    var comic by mutableStateOf<OpenedComic?>(null)
        private set

    var error by mutableStateOf<UiText?>(null)
        private set

    /** Set for an ID that isn't a comic: the content to show instead of the reader. */
    var redirect by mutableStateOf<String?>(null)
        private set

    /** The parent series, whose `meta` gives the Auto direction and the item labels. */
    var series by mutableStateOf<Content?>(null)
        private set

    var settings by mutableStateOf(ReaderSettings())
        private set

    /**
     * 0-based. Not in saved state: after process death the sync opens the reader where its lane
     * stands, the phone's unsent page or else the server's, which may be newer (P2 §6).
     */
    var page by mutableIntStateOf(0)
        private set

    /** Counts the placements: each shows its spread from the start, also within the current spread. */
    var placement by mutableIntStateOf(0)
        private set

    /** Past the last page, on the end card. */
    var atEnd by mutableStateOf(false)
        private set

    var siblings by mutableStateOf(Siblings(Siblings.Status.Loading))
        private set

    /** The previous or next volume, when it can't be opened now (offline and not downloaded). It is named, never skipped. */
    var unreadable by mutableStateOf(emptySet<String>())
        private set

    /** The series' earliest unread volume before this one, for the end card's "Read earlier volume"; asked online at the series' last volume. */
    var earlierUnread by mutableStateOf<String?>(null)
        private set

    /** A status row action is under way. */
    var statusBusy by mutableStateOf(false)
        private set

    private val _messages = Channel<UiText>(Channel.BUFFERED)

    /** Snackbars: why a status row action failed. */
    val messages = _messages.receiveAsFlow()

    val readablePrev: Content? get() = siblings.prev?.takeIf { it.id !in unreadable }
    val readableNext: Content? get() = siblings.next?.takeIf { it.id !in unreadable }

    private val pageCount get() = comic?.pages?.pages?.size ?: 0
    private var siblingsJob: Job? = null
    private val preloads = mutableMapOf<Int, Job>()
    private val preloadSlots = Semaphore(PRELOAD_CONCURRENCY)

    /**
     * Where the longstrip showed within its page, kept across a rotation, which recreates the reader but not this
     * model. Not state, and not saved: a placement or a new copy clears it, and after process death the sync opens the page.
     */
    var stripAnchor: StripAnchor? = null

    /** Opened and not yet placed, or placed: the adapter compares positions by its pages. State, as [title] reads it. */
    private var opened by mutableStateOf<OpenedComic?>(null)

    /** The item's title, once opened: the clear confirmation names it. */
    val title: String get() = opened?.content?.title.orEmpty()

    /** Attached once; Retry only loads it again. */
    val sync: ReadingSession = readingSync.attach(
        contentId,
        ComicAdapter(pages = { opened?.pages?.pages?.size ?: 0 }, content = { opened?.content }, restore = ::restore),
    )

    /** Its end lets go of the shown comic's files. */
    private var copyScope: CoroutineScope? = null

    private fun newCopyScope() = CoroutineScope(viewModelScope.coroutineContext + Job(viewModelScope.coroutineContext.job))

    private fun adopt(scope: CoroutineScope) {
        copyScope?.cancel()
        copyScope = scope
    }

    private var loadJob: Job? = null

    /** Counts the loads: only the latest one publishes, whatever order they finish in. */
    private var loads = 0

    fun load() {
        error = null
        needsServer = false
        // One open at a time owns the copy scope: a load supersedes a reopen, and the other way round.
        loadJob?.cancel()
        reopenJob?.cancel()
        val mine = ++loads
        val scope = newCopyScope()
        loadJob = viewModelScope.launch {
            try {
                val failure = attemptResult {
                    val opened = data.open(contentId, scope)
                    val content = opened.content
                    // Cards never send a series or a book here; this is for an ID of unknown type.
                    if (content.type != ContentType.COMIC) {
                        redirect = content.id
                        return@attemptResult
                    }
                    // Before the pages show, or an RTL series would open left to right and flip.
                    content.parentId?.let { attempt { series = data.content(it) } }
                    settings = settingsStore.settings.first()
                    if (loads != mine) return@attemptResult
                    this@ReaderViewModel.opened = opened
                    val count = opened.pages.pages.size
                    // Reconciled with the server and the outbox: where the reader opens.
                    // Gone from the server (a download read on): where the lane last stood, which the local copy carries.
                    val saved = try {
                        sync.load()
                    } catch (e: ReadingFailure.Gone) {
                        content.userData?.progress ?: EmptyProgress
                    }
                    if (loads != mine) return@attemptResult
                    page = pageFor(saved, count)
                    // A Retry after a failed reopen may come from the old copy's end card.
                    atEnd = false
                    comic = opened
                    stripAnchor = null
                    sync.placed(position(page, count))
                    adopt(scope)
                    retirePreloads()
                    preload()
                    watchReplaced(opened, scope)
                    loadSiblings(content.parentId, reread = false)
                }.exceptionOrNull()
                if (loads == mine) {
                    val shown = failure?.let { openError(it) }
                    // It suspends: a newer load may have cleared the error meanwhile.
                    if (loads == mine) error = shown
                }
            } finally {
                // Not shown: failed, redirected or cancelled.
                if (copyScope !== scope) {
                    scope.cancel()
                    // The adapter was given this copy for the sync's load; the shown one is the adapter's again.
                    if (loads == mine) opened = comic
                }
            }
        }
    }

    private fun retirePreloads() {
        preloads.values.forEach { it.cancel() }
        preloads.clear()
    }

    /** The open didn't reach the server, and there is no copy here: it opens once the server is back. */
    private var needsServer = false

    /** An item that isn't downloaded can't be opened without the server: it says so rather than why the request failed (P2 §10). */
    private suspend fun openError(e: Throwable): UiText {
        needsServer = e.isUnreachable() && contentId in data.unreadable(setOf(contentId), unreachable = true).first()
        return if (needsServer) UiText.Res(R.string.reader_connect_to_read) else e.toUiText()
    }

    private var reopenJob: Job? = null

    /** Counts the reopens: only the latest one ends [reopening]. */
    private var reopens = 0

    /** A reopen is under way: the sync's restores are ignored, while the user's own moves still apply to what shows. */
    private var reopening = false

    /** For as long as [opened] is the shown comic. */
    private fun watchReplaced(opened: OpenedComic, scope: CoroutineScope) {
        scope.launch { if (opened.replaced.firstOrNull() != null) onReplaced() }
    }

    /**
     * A downloaded copy replaced or deleted under the reader: it opens again, from the network or
     * the new files, as one placement on the same page (clamped to the new count). The end card
     * stays only while the reader is still at the new end. The old comic shows, and keeps its
     * files, until the new one is ready; the session, and its queued reading, stay. On the main
     * thread.
     */
    private fun onReplaced() {
        // Before anything suspends: a restore already posted would place the old comic's page on the new one.
        sync.invalidateRestores()
        reopening = true
        reopenJob?.cancel()
        loadJob?.cancel()
        val gen = ++loads
        val next = newCopyScope()
        // Not the job: on the immediate dispatcher the reopen may end before launch returns it.
        val mine = ++reopens
        reopenJob = viewModelScope.launch {
            try {
                val again = data.open(contentId, next)
                val count = again.pages.pages.size
                // The lane must know the new count before a position is placed against it.
                sync.pageCount(count)
                if (loads != gen) return@launch
                // From here to the end nothing suspends: one publication.
                val before = pageCount
                retirePreloads()
                opened = again
                comic = again
                stripAnchor = null
                page = page.coerceIn(0, (count - 1).coerceAtLeast(0))
                if (count > before) atEnd = false
                placement++
                sync.placed(position(page, count))
                adopt(next)
                preload()
                watchReplaced(again, next)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                if (loads != gen) return@launch
                // Deleted while offline, the count not stored, or the account gone: Retry loads anew.
                comic = null
                opened = null
                error = e.toUiText()
                copyScope?.cancel()
                copyScope = null
            } finally {
                if (reopens == mine) reopening = false
                if (copyScope !== next) next.cancel()
            }
        }
    }

    // Mode keeps the web's `parent_id || ''` key; direction gives a parentless comic its own entry.
    private val modeKey get() = comic?.content?.parentId.orEmpty()
    private val directionKey get() = comic?.content?.let { it.parentId ?: it.id }.orEmpty()

    /** What this comic's series overrides; a null field is Auto. */
    val seriesSettings: SeriesSettings
        get() = SeriesSettings(settings.seriesSettings[modeKey]?.mode, settings.seriesSettings[directionKey]?.direction)

    val autoMode: ReaderMode get() = detectMode(comic?.pages?.pages.orEmpty())
    val autoDirection: ReadingDirection get() = detectDirection(comic?.content?.meta, series?.meta)
    val mode: ReaderMode get() = seriesSettings.mode ?: autoMode
    val direction: ReadingDirection get() = seriesSettings.direction ?: autoDirection
    val controlsFlipped: Boolean get() = controlsFlipped(mode, direction, settings)
    val shifted: Boolean get() = comic?.content?.id in settings.shiftedBooks

    /**
     * A change from the settings sheet. It lays the page out anew, so it is a placement, counted
     * with the stored settings as they arrive: the page is placed once, in its new layout.
     */
    fun changeSettings(change: (ReaderSettings) -> ReaderSettings) {
        if (comic == null) return
        viewModelScope.launch {
            val before = mode
            settings = settingsStore.update(change) ?: return@launch
            // The strip leaves the end set: in another mode the reader shows its page, not the end card.
            if (mode != before) atEnd = false
            placement++
            sync.placed(position(page, pageCount))
        }
    }

    fun setMode(mode: ReaderMode?) {
        val key = modeKey
        changeSettings { it.updateSeriesSettings(key) { entry -> entry.copy(mode = mode) } }
    }

    fun setDirection(direction: ReadingDirection?) {
        val key = directionKey
        changeSettings { it.updateSeriesSettings(key) { entry -> entry.copy(direction = direction) } }
    }

    fun toggleShift() {
        val id = comic?.content?.id ?: return
        changeSettings { it.copy(shiftedBooks = if (id in it.shiftedBooks) it.shiftedBooks - id else it.shiftedBooks + id) }
    }

    /** The reader came to rest on [index] through the user's input: reading. */
    fun onPageSettled(index: Int) {
        if (moveTo(index)) sync.moved(position(page, pageCount))
    }

    /** The end card was reached, or the bottom of a longstrip. */
    fun onReachedEnd() {
        if (comic == null) return
        atEnd = true
        sync.finish(finishProgress(pageCount))
    }

    /** A placement (the slider), which is not reading. */
    fun placePage(index: Int) {
        if (!moveTo(index)) return
        placement++
        sync.placed(position(page, pageCount))
    }

    /** The sync places the reader: another device's position, or Undo. Before it has opened, the sync opens it there instead. */
    private fun restore(index: Int) {
        if (comic != null && !reopening) placePage(index)
    }

    private var visible = false

    /** The screen resumed or paused; a rotation reports neither, as the web gets no event on a resize. */
    fun setVisible(visible: Boolean) {
        if (visible == this.visible) return
        this.visible = visible
        sync.setVisible(visible)
    }

    override fun onCleared() {
        sync.detach()
    }

    /** The status row's [action]; a failure is a snackbar, as on the web. */
    fun runStatus(action: StatusAction) {
        if (statusBusy) return
        statusBusy = true
        viewModelScope.launch {
            val result = attemptResult {
                when (action) {
                    StatusAction.TRACK -> sync.trackProgress()
                    StatusAction.RESET -> sync.resetAndReadAgain()
                    StatusAction.RESUME_SERIES -> sync.seriesCommand(ReadingStatus.READING)
                    StatusAction.COMPLETE -> sync.command(request("mark_completed"))
                    StatusAction.READING -> sync.command(request("set_status", ReadingStatus.READING))
                }
            }
            statusBusy = false
            val failure = result.exceptionOrNull()
            when {
                failure != null -> _messages.send(UiText.Format(R.string.reader_update_failed, listOf(failure.toUiText())))
                // Queued in memory only, until space is back.
                result.getOrNull() == CommandOutcome.UNSAVED -> _messages.send(UiText.Res(R.string.error_not_saved_storage_full))
            }
        }
    }

    private fun request(op: String, status: String? = null) =
        JsonObject(listOfNotNull("op" to JsonPrimitive(op), status?.let { "status" to JsonPrimitive(it) }).toMap())

    fun retrySiblings() {
        val content = comic?.content ?: return
        if (siblings.status == Siblings.Status.Error) loadSiblings(content.parentId, reread = true)
    }

    /** The web store's `setPage`: an unchanged page does nothing. True when the reader moved. */
    private fun moveTo(index: Int): Boolean {
        if (comic == null) return false
        val leftEnd = atEnd
        atEnd = false
        val clamped = index.coerceIn(0, (pageCount - 1).coerceAtLeast(0))
        if (clamped == page) return leftEnd
        page = clamped
        preload()
        return true
    }

    private fun loadSiblings(parentId: String?, reread: Boolean) {
        siblingsJob?.cancel()
        siblings = Siblings(Siblings.Status.Loading)
        earlierUnread = null
        siblingsJob = viewModelScope.launch {
            val loaded = loadSiblings(data, contentId, parentId, reread)
            unreadable = emptySet()
            siblings = loaded
            // The end card of a series' last volume, asked again when the series' reading changes or the server is back.
            if (loaded.status == Siblings.Status.Ready && loaded.next == null && parentId != null) {
                launch {
                    combine(sync.view.map { it.series }, connectivity.online, ::Pair).distinctUntilChanged().collectLatest { (_, online) ->
                        if (online) earlierUnread = attemptResult { data.earlierUnread(parentId) }.getOrNull()
                    }
                }
            }
            // Follows the connection and the downloads for as long as these are the siblings.
            data.unreadable(setOfNotNull(loaded.prev?.id, loaded.next?.id)).collect { unreadable = it }
        }
    }

    /** Goes to the previous or next volume if it can be opened now; asked again at the moment it is chosen. */
    fun openVolume(id: String, open: (String) -> Unit) {
        viewModelScope.launch { if (attemptResult { data.readable(id) }.getOrDefault(false)) open(id) }
    }

    /** Keeps the pages around the current one coming, a few at a time, and drops distant ones. */
    private fun preload() {
        val pages = comic?.pages ?: return
        preloads.entries.removeAll { (index, job) ->
            (abs(index - page) > PRELOAD_COUNT).also { if (it) job.cancel() }
        }
        for (index in getPagesInPreloadOrder(pageCount, page).take(PRELOAD_COUNT)) {
            if (preloads[index]?.isCancelled == false) continue
            preloads[index] = viewModelScope.launch { preloadSlots.withPermit { pages.preloadQuietly(index) } }
        }
    }

    // Last: on an immediate dispatcher load() runs into the state declared above.
    init {
        load()
        // "Not downloaded. Connect to read." opens once the server is back.
        viewModelScope.launch { connectivity.online.drop(1).collect { if (it && needsServer) load() } }
    }
}
