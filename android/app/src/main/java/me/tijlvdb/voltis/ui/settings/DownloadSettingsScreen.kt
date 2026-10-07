package me.tijlvdb.voltis.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.data.downloads.DownloadSnapshot
import me.tijlvdb.voltis.data.settings.DeviceSettings
import me.tijlvdb.voltis.ui.OfflineBar
import me.tijlvdb.voltis.ui.PushedScreen
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.downloads.DownloadList
import me.tijlvdb.voltis.ui.downloads.downloadList
import me.tijlvdb.voltis.ui.downloads.fileSize
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VDialog
import me.tijlvdb.voltis.ui.kit.VSpinner
import me.tijlvdb.voltis.ui.kit.VSwitchRow

/** Bytes on disk with whether anything is downloaded or queued, and the free bytes. */
class DownloadStored(val list: DownloadList, val free: Long)

/** The Wi-Fi setting, the other accounts' bytes, and the open store's [stored]: null when that store couldn't be opened. */
class DownloadSettingsContent(val stored: DownloadStored?, val wifiOnly: Boolean, val others: Long?)

/**
 * Ready once the other accounts' directories are read and the open account's store is either read (and is [account]'s) or
 * failed ([failed] is that account; another's is no concern of this page): that failure is final, and the other accounts' data is the way to free the space it may need. Null before that.
 */
internal fun settingsContent(snapshot: DownloadSnapshot?, failed: String?, account: String?, wifiOnly: Boolean, others: Long?, othersRead: Boolean): DownloadSettingsContent? = when {
    !othersRead -> null
    failed != null && failed == account -> DownloadSettingsContent(null, wifiOnly, others)
    snapshot == null || snapshot.account != account -> null
    else -> DownloadSettingsContent(DownloadStored(downloadList(snapshot.rows, snapshot.detached), snapshot.free), wifiOnly, others)
}

/** What the other accounts' directories held when read: [bytes] null for none. The read not being there yet is a null [Others]. */
private class Others(val bytes: Long?)

@HiltViewModel
class DownloadSettingsViewModel @Inject constructor(
    private val downloads: DownloadRepository,
    private val stores: AccountStores,
    private val device: DeviceSettings,
    session: SessionStore,
) : ViewModel() {
    /** The signed-in account: its data isn't another account's, even when its store couldn't be opened. */
    private val account = session.state.value.account

    private val othersRead = MutableStateFlow<Others?>(null)

    /** Raised by [load]: the free space is read again. */
    private val refresh = MutableStateFlow(0)

    /** What the page shows about the open store, null until all of it is read (and again when the store changes). */
    val content = combine(downloads.snapshot(), downloads.failed, device.downloadWifiOnly, othersRead, refresh) { snapshot, failed, wifiOnly, others, _ ->
        // Free space is read again on each refresh, while the content stays as it is.
        val fresh = snapshot?.let { s -> downloads.freeBytes()?.let { DownloadSnapshot(s.account, s.rows, s.detached, s.policies, it) } } ?: snapshot
        settingsContent(fresh, failed, account, wifiOnly, others?.bytes, others != null)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)
    val failed = downloads.failed


    fun load() {
        viewModelScope.launch {
            // An unresolvable accounts directory or an unreadable file: the last values stay.
            try {
                othersRead.value = Others(stores.others(account).takeIf { it.isNotEmpty() }?.sumOf { it.bytes })
            } catch (e: kotlinx.coroutines.CancellationException) {
                throw e
            } catch (e: Exception) {
                android.util.Log.e("DownloadSettings", "Couldn't read the storage", e)
                // Not read ever: none, so the page isn't held for it.
                if (othersRead.value == null) othersRead.value = Others(null)
            } finally {
                refresh.value++
            }
        }
    }

    fun setWifiOnly(on: Boolean) {
        viewModelScope.launch { device.setDownloadWifiOnly(on) }
    }

    /** Downloads only: the outbox and the database stay (P2 §3). */
    fun deleteAll() {
        viewModelScope.launch {
            // A refused delete (a full disk) is no success: the message says so, and nothing else changes.
            attempt {
                downloads.deleteAll()
            }?.let { _messages.trySend(it) }
        }
    }

    private val _messages = Channel<UiText>(Channel.BUFFERED)
    val messages = _messages.receiveAsFlow()

    fun deleteOthers() {
        viewModelScope.launch {
            try {
                stores.deleteOthers(account)
            } catch (e: kotlinx.coroutines.CancellationException) {
                throw e
            } catch (e: Exception) {
                // An unresolvable accounts directory, say: nothing was deleted.
                android.util.Log.e("DownloadSettings", "Couldn't delete the other accounts' data", e)
            }
            load()
        }
    }
}

/** Wi-Fi only, what downloads take, and deleting them or what other accounts left (P2 §11). */
@Composable
fun DownloadSettingsScreen(onBack: () -> Unit, vm: DownloadSettingsViewModel = hiltViewModel()) {
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.load() }
    val context = LocalContext.current
    val snackbars = LocalSnackbars.current
    LaunchedEffect(vm) { vm.messages.collect { snackbars.show(it.string(context)) } }
    val content by vm.content.collectAsStateWithLifecycle()
    var deleting by rememberSaveable { mutableStateOf<Deleting?>(null) }
    PushedScreen(stringResource(R.string.settings_downloads), onBack) {
        OfflineBar()
        val ready = content
        val stored = ready?.stored
        if (ready == null) {
            VSpinner()
        } else if (stored == null) {
            QueryError(UiText.Res(R.string.error_offline_data))
        } else {
            VSwitchRow(stringResource(R.string.downloads_wifi_only), ready.wifiOnly, vm::setWifiOnly)
            Text(stringResource(R.string.downloads_storage, fileSize(stored.list.bytes), fileSize(stored.free)), color = MaterialTheme.colorScheme.onSurfaceVariant)
            VButton(
                stringResource(R.string.download_settings_delete_all),
                { deleting = Deleting.DOWNLOADS },
                style = VButtonStyle.Tonal,
                enabled = stored.list.queue.isNotEmpty() || stored.list.groups.isNotEmpty(),
            )
        }
        content?.others?.let { bytes ->
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(stringResource(R.string.download_settings_others), style = MaterialTheme.typography.bodyLarge)
                    Text(fileSize(bytes), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
                }
                VButton(stringResource(R.string.downloads_delete), { deleting = Deleting.OTHERS }, style = VButtonStyle.Tonal)
            }
        }
    }
    when (deleting) {
        Deleting.DOWNLOADS -> VDialog(
            stringResource(R.string.downloads_delete_all_confirm),
            stringResource(R.string.downloads_delete),
            onConfirm = {
                deleting = null
                vm.deleteAll()
            },
            onDismiss = { deleting = null },
            text = stringResource(R.string.downloads_delete_all_text),
            danger = true,
        )
        Deleting.OTHERS -> VDialog(
            stringResource(R.string.download_settings_others_confirm),
            stringResource(R.string.downloads_delete),
            onConfirm = {
                deleting = null
                vm.deleteOthers()
            },
            onDismiss = { deleting = null },
            text = stringResource(R.string.download_settings_others_text),
            danger = true,
        )
        null -> Unit
    }
}

private enum class Deleting { DOWNLOADS, OTHERS }
