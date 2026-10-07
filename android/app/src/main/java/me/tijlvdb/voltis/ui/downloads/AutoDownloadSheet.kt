package me.tijlvdb.voltis.ui.downloads

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.attemptResult
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.domain.downloads.SeriesPolicy
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VIconButton
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VSheet
import me.tijlvdb.voltis.ui.kit.VSwitchRow

const val AUTO_DEFAULT_KEEP = 3
const val AUTO_MAX_KEEP = 10

/** The policy a sheet's choices make, or null (no policy) when both switches are off. */
fun policyOf(seriesId: String, keep: Boolean, keepNext: Int, deleteFinished: Boolean): SeriesPolicy? =
    if (!keep && !deleteFinished) null else SeriesPolicy(seriesId, if (keep) keepNext.coerceIn(1, AUTO_MAX_KEEP) else 0, deleteFinished)

@HiltViewModel
class AutoDownloadViewModel @Inject constructor(private val downloads: DownloadRepository) : ViewModel() {
    var keep by mutableStateOf(false)
        private set
    var keepNext by mutableIntStateOf(AUTO_DEFAULT_KEEP)
        private set
    var deleteFinished by mutableStateOf(false)
        private set

    /** Finished volumes "delete after finishing" would delete now. */
    var deletes by mutableIntStateOf(0)
        private set

    /** Fetching the series, then saving. */
    var saving by mutableStateOf(false)
        private set
    var error by mutableStateOf<UiText?>(null)
        private set
    private var job: Job? = null

    /** The sheet opens at the series' policy, else at the defaults. */
    fun load(seriesId: String) {
        job?.cancel()
        saving = false
        error = null
        job = viewModelScope.launch {
            attemptResult { downloads.policies.first()[seriesId] }
                .onSuccess { policy ->
                    keep = policy != null && policy.keepNext > 0
                    keepNext = policy?.keepNext?.takeIf { it > 0 } ?: AUTO_DEFAULT_KEEP
                    deleteFinished = policy?.deleteFinished == true
                    count(seriesId)
                }
                .onFailure { error = it.toUiText() }
        }
    }

    fun keepOn(on: Boolean) {
        keep = on
    }

    fun step(by: Int) {
        keepNext = (keepNext + by).coerceIn(1, AUTO_MAX_KEEP)
    }

    fun setDeleteFinished(seriesId: String, on: Boolean) {
        deleteFinished = on
        if (on) viewModelScope.launch { count(seriesId) } else deletes = 0
    }

    private suspend fun count(seriesId: String) {
        // Only a hint: a failed read shows none.
        deletes = if (deleteFinished) attemptResult { downloads.current.value?.let { downloads.finishedDeletable(it, seriesId) } ?: 0 }.getOrDefault(0) else 0
    }

    /** Saves the choices; [onSaved] when they took, else [error] says why. */
    fun save(seriesId: String, onSaved: () -> Unit) {
        if (saving) return
        val policy = policyOf(seriesId, keep, keepNext, deleteFinished)
        val owner = downloads.current.value ?: return
        saving = true
        error = null
        job = viewModelScope.launch {
            attemptResult { downloads.setPolicy(owner, seriesId, policy) }
                // Null: the account changed meanwhile, and nothing was saved.
                .onSuccess { if (it != null) onSaved() else error = SyncUnavailable.AccountChanged().toUiText() }
                .onFailure { error = it.toUiText() }
            saving = false
        }
    }
}

/**
 * A series' automatic downloads (P2 §17): keep the next N volumes downloaded, and delete volumes once
 * they are finished. Both off removes the policy. Save fetches the series first ("Loading…").
 */
@Composable
fun AutoDownloadSheet(seriesId: String, onDismiss: () -> Unit, vm: AutoDownloadViewModel = hiltViewModel()) {
    LaunchedEffect(seriesId) { vm.load(seriesId) }
    VSheet(stringResource(R.string.downloads_auto), onDismiss) {
        Column(Modifier.fillMaxWidth()) {
            VSwitchRow(stringResource(R.string.downloads_auto_keep), vm.keep, vm::keepOn)
            if (vm.keep) {
                Row(Modifier.fillMaxWidth().heightIn(min = 48.dp), Arrangement.SpaceBetween, Alignment.CenterVertically) {
                    Text(stringResource(R.string.downloads_auto_volumes), style = MaterialTheme.typography.bodyLarge)
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        VIconButton(VIcons.Minus, stringResource(R.string.downloads_auto_fewer), { vm.step(-1) }, enabled = vm.keepNext > 1)
                        Text(
                            vm.keepNext.toString(),
                            Modifier.semantics { liveRegion = LiveRegionMode.Polite },
                            style = MaterialTheme.typography.titleMedium,
                        )
                        VIconButton(VIcons.Plus, stringResource(R.string.downloads_auto_more), { vm.step(1) }, enabled = vm.keepNext < AUTO_MAX_KEEP)
                    }
                }
            }
            VSwitchRow(stringResource(R.string.downloads_auto_delete), vm.deleteFinished, { vm.setDeleteFinished(seriesId, it) })
            if (vm.deleteFinished && vm.deletes > 0) {
                Text(
                    pluralStringResource(R.plurals.downloads_auto_deletes, vm.deletes, vm.deletes),
                    Modifier.semantics { liveRegion = LiveRegionMode.Polite },
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
            QueryError(vm.error, Modifier.padding(top = 8.dp), retry = { vm.save(seriesId, onDismiss) })
            VButton(
                stringResource(if (vm.saving) R.string.downloads_auto_saving else R.string.save),
                { vm.save(seriesId, onDismiss) },
                Modifier.padding(top = 4.dp).align(Alignment.End),
                enabled = !vm.saving,
            )
        }
    }
}
