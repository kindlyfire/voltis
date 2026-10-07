package me.tijlvdb.voltis.ui.downloads

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.ProgressBarRangeInfo
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.progressBarRangeInfo
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.data.settings.DeviceSettings
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.Stale
import me.tijlvdb.voltis.domain.downloads.offersAgain
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VDialog
import me.tijlvdb.voltis.ui.kit.VIconButton
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VProgressBar

@HiltViewModel
class DownloadButtonViewModel @Inject constructor(
    private val downloads: DownloadRepository,
    private val settings: DeviceSettings,
    connectivity: Connectivity,
) : ViewModel() {
    /** Download again needs the server: without it, it would only queue a wait. */
    val online = connectivity.online

    fun state(id: String) = downloads.download(id)

    fun series(seriesId: String) = downloads.series(seriesId)

    /** The series has automatic downloads. */
    fun auto(seriesId: String) = downloads.policies.map { seriesId in it }

    /** [onError] gets the message when it couldn't be queued (the series couldn't be fetched). */
    fun download(content: Content, onError: (UiText) -> Unit) {
        viewModelScope.launch { attempt { downloads.enqueue(listOf(content)) }?.let(onError) }
    }

    /** The notification permission is asked at the first download only. */
    suspend fun notificationsAsked() = settings.notificationsAsked.first()

    suspend fun markNotificationsAsked() = settings.markNotificationsAsked()

    fun cancel(id: String) = act { downloads.cancel(id) }

    fun pause(id: String) = act { downloads.pause(id) }

    fun resume(id: String) = act { downloads.resume(id) }

    fun retry(id: String) = act { downloads.retry(id) }

    fun again(id: String) = act { downloads.downloadAgain(id) }

    fun delete(id: String) = act { downloads.delete(listOf(id)) }

    /** Why a control didn't take (a full disk, a queue that couldn't start), for a snackbar. */
    private val _messages = Channel<UiText>(Channel.BUFFERED)
    val messages = _messages.receiveAsFlow()

    private fun act(block: suspend () -> Unit) {
        viewModelScope.launch { attempt(block)?.let { _messages.trySend(it) } }
    }
}

/**
 * A volume's download (P2 §11): Download; Downloaded with Delete, and when stale, why, with
 * "Download again" for a new version; the progress of a download or replacement, with Pause or Resume and Cancel.
 */
@Composable
fun DownloadButton(content: Content, onDelete: () -> Unit, modifier: Modifier = Modifier, vm: DownloadButtonViewModel = hiltViewModel()) {
    val id = content.id
    val row by remember(id) { vm.state(id) }.collectAsStateWithLifecycle(null)
    val online by vm.online.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val snackbars = LocalSnackbars.current
    val askNotifications = rememberAskNotifications(vm)
    LaunchedEffect(vm) { vm.messages.collect { snackbars.show(it.string(context)) } }
    val download = row
    if (download == null) {
        VButton(
            stringResource(R.string.downloads_download),
            {
                askNotifications()
                vm.download(content) { snackbars.show(it.string(context)) }
            },
            modifier,
            VButtonStyle.Tonal,
            icon = VIcons.Download,
        )
        return
    }
    val state = download.state
    Column(modifier, Arrangement.spacedBy(8.dp)) {
        if (download.copyId != null) {
            Row(Modifier.fillMaxWidth(), Arrangement.spacedBy(8.dp), Alignment.CenterVertically) {
                val icon = when (download.stale) {
                    Stale.VERSION -> VIcons.Updated
                    Stale.DAMAGED -> VIcons.AlertCircle
                    Stale.GONE -> VIcons.CloudOff
                    else -> VIcons.DownloadDone
                }
                Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
                Column(Modifier.weight(1f).semantics(mergeDescendants = true) {}) {
                    Text(stringResource(R.string.downloads_downloaded_size, fileSize(download.copyBytes)))
                    staleLabel(download.stale)?.let {
                        Text(it, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
                    }
                }
                VButton(stringResource(R.string.downloads_delete), onDelete, style = VButtonStyle.Text)
            }
            if (state == DownloadState.DONE && download.stale.offersAgain() && online) {
                VButton(stringResource(R.string.downloads_download_again), { vm.again(id) }, Modifier.fillMaxWidth(), VButtonStyle.Tonal, icon = VIcons.Download)
            }
        }
        // A download, or the replacement of the copy above.
        if (state != DownloadState.DONE) {
            Row(Modifier.fillMaxWidth(), Arrangement.spacedBy(4.dp), Alignment.CenterVertically) {
                Column(Modifier.weight(1f).semantics(mergeDescendants = true) {}, Arrangement.spacedBy(4.dp)) {
                    if (state == DownloadState.RUNNING || state == DownloadState.QUEUED) {
                        VProgressBar(
                            download.fraction,
                            Modifier.fillMaxWidth().padding(top = 4.dp).semantics { progressBarRangeInfo = ProgressBarRangeInfo(download.fraction, 0f..1f) },
                        )
                    }
                    Text(
                        downloadStatus(download),
                        // Announced when the state changes, not on every page.
                        Modifier.semantics { if (state != DownloadState.RUNNING) liveRegion = LiveRegionMode.Polite },
                        color = if (state == DownloadState.FAILED) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
                        style = MaterialTheme.typography.bodyMedium,
                    )
                }
                when {
                    state == DownloadState.QUEUED || state == DownloadState.RUNNING -> {
                        if (download.retryable) VIconButton(VIcons.Refresh, retryLabel(download), { vm.retry(id) })
                        VIconButton(VIcons.PauseFilled, stringResource(R.string.downloads_pause), { vm.pause(id) })
                    }
                    state == DownloadState.PAUSED -> VIconButton(VIcons.PlayFilled, stringResource(R.string.downloads_resume), { vm.resume(id) })
                    download.retryable -> VIconButton(VIcons.Refresh, retryLabel(download), { vm.retry(id) })
                }
                VIconButton(VIcons.Close, stringResource(R.string.cancel), { vm.cancel(id) })
            }
        }
    }
}

/** A series' "3 of 12 downloaded · 210 MB", or "2 of 12 queued" until one is done; it opens the series in Downloads. */
@Composable
fun SeriesDownloads(series: Content, rows: List<DownloadEntity>, auto: Boolean, openDownloads: (String) -> Unit, modifier: Modifier = Modifier) {
    if (series.type != ContentType.COMIC_SERIES || rows.isEmpty()) return
    val done = rows.filter { it.copyId != null }
    val total = maxOf(series.childrenCount ?: 0, rows.size)
    val line = if (done.isEmpty()) {
        stringResource(R.string.downloads_series_queued, rows.size, total)
    } else {
        stringResource(R.string.downloads_series_line, done.size, total, fileSize(done.sumOf { it.copyBytes }))
    }
    VButton(
        if (auto) line + " · " + stringResource(R.string.downloads_auto_mark) else line,
        { openDownloads(series.id) },
        modifier,
        style = VButtonStyle.Text,
        icon = if (done.isEmpty()) VIcons.Queued else VIcons.DownloadDone,
        // One line, so the header can hold its slot before Room answers; TalkBack reads the whole label.
        singleLine = true,
    )
}

/**
 * Asks for the notification permission at the first download on API 33 and later. Its launcher and
 * scope belong to the caller's composition, which must outlive a sheet that calls it on its way out.
 * It counts as asked only once the request was launched.
 */
@Composable
fun rememberAskNotifications(vm: DownloadButtonViewModel = hiltViewModel()): () -> Unit {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val permission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {}
    return {
        scope.launch {
            val missing = Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
                ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
            if (missing && !vm.notificationsAsked()) {
                permission.launch(Manifest.permission.POST_NOTIFICATIONS)
                vm.markNotificationsAsked()
            }
        }
    }
}

/** Confirms deleting [content]'s download. Its state lives with the caller, above any layout branch. */
@Composable
fun DeleteDownloadDialog(content: Content, onDismiss: () -> Unit, vm: DownloadButtonViewModel = hiltViewModel()) {
    VDialog(
        stringResource(R.string.downloads_delete_one, content.title),
        stringResource(R.string.downloads_delete),
        onConfirm = {
            onDismiss()
            vm.delete(content.id)
        },
        onDismiss = onDismiss,
        text = stringResource(R.string.downloads_delete_text),
        danger = true,
    )
}
