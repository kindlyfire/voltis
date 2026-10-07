package me.tijlvdb.voltis.ui.downloads

import android.content.Context
import android.net.ConnectivityManager
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.db.DownloadRow
import me.tijlvdb.voltis.data.downloads.CatalogRefresher
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.data.settings.DeviceSettings
import me.tijlvdb.voltis.domain.downloads.DownloadError
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.ErrorKind
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.data.sync.SyncCenter

/** Downloaded copies of one series, or a standalone item ([standalone], one row). */
data class DownloadGroup(
    val seriesId: String,
    val title: String,
    val coverVersion: String?,
    val standalone: Boolean,
    val items: List<DownloadRow>,
    /** The series has a policy (P2 §17). */
    val auto: Boolean = false,
) {
    val bytes get() = items.sumOf { it.download.copyBytes }
}

/** What the page shows, all read for one open store; [free] is as of the last change to its rows. */
class DownloadsContent(val list: DownloadList, val free: Long, val unsent: Int, val attention: Int, val wifiOnly: Boolean)

/** What the queue is waiting for, when it can't run. */
enum class QueueNote { STORAGE, WIFI, CONNECTION }

/**
 * The queue in order, then the copies grouped by series; a replacement is in both. [bytes] is
 * everything on disk: copies, transfers, and [detached] directories until they are deleted.
 */
data class DownloadList(val queue: List<DownloadRow>, val groups: List<DownloadGroup>, val bytes: Long)

fun downloadList(rows: List<DownloadRow>, detached: Long = 0): DownloadList {
    val queue = rows.filter { it.download.state != DownloadState.DONE }
    val groups = rows.filter { it.download.copyId != null }.groupBy { it.download.seriesId }.map { (seriesId, items) ->
        val first = items.first()
        val standalone = first.download.contentId == seriesId
        DownloadGroup(
            seriesId,
            (if (standalone) first.title else first.seriesTitle) ?: seriesId,
            if (standalone) first.coverVersion else first.seriesCoverVersion,
            standalone,
            items.sortedWith(compareBy<DownloadRow, Int?>(nullsLast()) { it.position }.thenBy { it.title }),
        )
    }.sortedBy { it.title.lowercase() }
    return DownloadList(queue, groups, rows.sumOf { it.download.copyBytes + it.download.bytesDone } + detached)
}

fun queueNote(queue: List<DownloadRow>, wifiOnly: Boolean, metered: Boolean, online: Boolean): QueueNote? {
    val rows = queue.map { it.download }
    return when {
        rows.any { it.state == DownloadState.FAILED && it.errorKind == ErrorKind.STORAGE } &&
            rows.any { it.state == DownloadState.PAUSED } -> QueueNote.STORAGE
        rows.none { it.state == DownloadState.QUEUED || it.state == DownloadState.RUNNING } -> null
        // First: without a network the platform reports it metered.
        !online -> QueueNote.CONNECTION
        wifiOnly && metered -> QueueNote.WIFI
        rows.any { it.state == DownloadState.QUEUED && it.error == DownloadError.UNREACHABLE } -> QueueNote.CONNECTION
        else -> null
    }
}

@HiltViewModel
class DownloadsViewModel @Inject constructor(
    private val downloads: DownloadRepository,
    private val refresher: CatalogRefresher,
    settings: DeviceSettings,
    connectivity: Connectivity,
    private val sync: SyncCenter,
    session: SessionStore,
    @param:ApplicationContext private val context: Context,
) : ViewModel() {
    /** The account the screen was opened for. */
    private val account = session.state.value.account

    /**
     * Everything the page shows, read for the open store: null until all of it is, and again when the store or the
     * sync owners change, so the page draws once and nothing already shown moves.
     */
    val content = combine(
        downloads.snapshot(),
        account?.let(sync::attentionCountOrNull) ?: flowOf(0),
        sync.unsentOrNull,
        settings.downloadWifiOnly,
    ) { snapshot, attention, unsent, wifiOnly ->
        if (snapshot == null || attention == null || unsent == null || snapshot.account != account || unsent.account != account) return@combine null
        val list = downloadList(snapshot.rows, snapshot.detached).let { list -> list.copy(groups = list.groups.map { it.copy(auto = it.seriesId in snapshot.policies) }) }
        DownloadsContent(list, snapshot.free, unsent.value, attention, wifiOnly)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    /** The signed-in account whose store couldn't be opened. */
    val failed = downloads.failed

    fun retryOpen() = downloads.retryOpen()
    val online = connectivity.online

    var syncing by mutableStateOf(false)
        private set

    /** "Sync now": everything unwatched goes again (`drain()`); the line follows the count. */
    fun syncNow() {
        if (syncing) return
        syncing = true
        viewModelScope.launch {
            try {
                account?.let { sync.drain(it) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                // No engine (an account change): nothing to send.
            } finally {
                syncing = false
            }
        }
    }

    /** Read when the screen resumes and every few seconds while it shows; Connectivity follows only the default network's presence, not its kind. */
    var metered by mutableStateOf(context.getSystemService(ConnectivityManager::class.java).isActiveNetworkMetered)
        private set

    fun check() {
        metered = context.getSystemService(ConnectivityManager::class.java).isActiveNetworkMetered
    }

    /** Pull-to-refresh: the rows and stale marks again from the server. */
    var refreshing by mutableStateOf(false)
        private set

    fun refresh() {
        if (refreshing) return
        refreshing = true
        viewModelScope.launch {
            try {
                refresher.refresh(automatic = false).join()
            } finally {
                refreshing = false
            }
        }
    }

    fun again(id: String) = act { downloads.downloadAgain(id) }

    fun pause(id: String) = act { downloads.pause(id) }

    fun resume(id: String) = act { downloads.resume(id) }

    fun retry(id: String) = act { downloads.retry(id) }

    fun cancel(id: String) = act { downloads.cancel(id) }

    fun pauseAll() = act { downloads.pauseAll() }

    fun resumeAll() = act { downloads.resumeAll() }

    fun delete(ids: List<String>) = act {
        downloads.delete(ids)
    }

    fun deleteAll() = act {
        downloads.deleteAll()
    }

    /** Why a control didn't take (a full disk, a queue that couldn't start), for a snackbar. */
    private val _messages = Channel<UiText>(Channel.BUFFERED)
    val messages = _messages.receiveAsFlow()

    private fun act(block: suspend () -> Unit) {
        viewModelScope.launch { attempt(block)?.let { _messages.trySend(it) } }
    }
}
