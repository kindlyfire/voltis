package me.tijlvdb.voltis.ui.content

import androidx.activity.compose.LocalActivity
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.lists.entryItem
import me.tijlvdb.voltis.domain.catalog.hasVolumes
import me.tijlvdb.voltis.domain.catalog.isSeries
import me.tijlvdb.voltis.domain.catalog.itemName
import me.tijlvdb.voltis.ui.itemLabels
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VCheckboxRow
import me.tijlvdb.voltis.ui.kit.VDialog
import me.tijlvdb.voltis.ui.kit.VSpinner
import me.tijlvdb.voltis.ui.lists.ListsSheet
import me.tijlvdb.voltis.ui.lists.LISTS_SHEET
import me.tijlvdb.voltis.ui.lists.ListsSheetViewModel
import me.tijlvdb.voltis.ui.reader.VolumeSheet

/** The open dialog of a content page. Each stays open on a failure and shows it. */
@Composable
fun ContentDialogs(content: Content, vm: ContentViewModel) {
    when (val dialog = vm.dialog) {
        is ContentDialog.Clear -> ClearDialog(content, vm)
        ContentDialog.Complete -> CompleteDialog(content, vm)
        ContentDialog.UpdateProgress -> UpdateProgressDialog(content, vm)
        is ContentDialog.Lists -> content.entryItem()?.let { item ->
            val sheet = hiltViewModel<ListsSheetViewModel>(key = LISTS_SHEET)
            val activity = LocalActivity.current
            DisposableEffect(dialog.opening) {
                sheet.open(dialog.opening)
                onDispose { if (activity?.isChangingConfigurations != true) sheet.close(dialog.opening) }
            }
            ListsSheet(listOf(item), selection = false, sheet, onDismiss = vm::dismiss)
        }
        null -> {}
    }
}

/** The port of `ClearReadingModal.vue`: names what it clears. */
@Composable
private fun ClearDialog(content: Content, vm: ContentViewModel) {
    val count = content.childrenCount ?: 0
    val text = if (content.isSeries) {
        pluralStringResource(if (content.hasVolumes) R.plurals.clear_series_volumes else R.plurals.clear_series_chapters, count, count)
    } else {
        stringResource(R.string.clear_item, content.title)
    }
    VDialog(
        stringResource(R.string.content_clear),
        stringResource(R.string.clear_confirm),
        vm::clear,
        vm::dismiss,
        text,
        danger = true,
        busy = vm.busy,
        error = vm.dialogError,
    )
}

/** The port of `MarkSeriesCompletedModal.vue`. */
@Composable
private fun CompleteDialog(series: Content, vm: ContentViewModel) {
    var includeUnread by rememberSaveable { mutableStateOf(false) }
    val volumes = series.hasVolumes
    val unread = series.unreadChildrenCount ?: 0
    VDialog(
        stringResource(R.string.complete_title),
        stringResource(R.string.status_mark_completed),
        { vm.markSeriesCompleted(includeUnread) },
        vm::dismiss,
        stringResource(if (volumes) R.string.complete_volumes else R.string.complete_chapters),
        busy = vm.busy,
        error = vm.dialogError,
    ) {
        if (unread > 0) {
            val label = pluralStringResource(if (volumes) R.plurals.complete_include_volumes else R.plurals.complete_include_chapters, unread, unread)
            VCheckboxRow(label, includeUnread, { includeUnread = it }, enabled = !vm.busy)
        }
    }
}

private enum class ProgressAction(val label: Int) {
    Clear(R.string.content_clear),
    MarkAll(R.string.update_mark_all),
    MarkThrough(R.string.update_mark_through),
}

/** The port of `UpdateProgressModal.vue`. Clearing hands over to the Clear dialog, which names what it clears. */
@Composable
private fun UpdateProgressDialog(series: Content, vm: ContentViewModel) {
    var action by rememberSaveable { mutableStateOf<ProgressAction?>(null) }
    var chosen by rememberSaveable { mutableStateOf<String?>(null) }
    var picking by rememberSaveable { mutableStateOf(false) }
    val volumes = series.hasVolumes
    val list = vm.volumes
    // A volume that has gone since it was chosen is no choice.
    val until = list?.indexOfFirst { it.id == chosen } ?: -1
    VDialog(
        stringResource(R.string.content_update_progress),
        stringResource(R.string.confirm),
        onConfirm = {
            when (action) {
                ProgressAction.Clear -> vm.show(ContentDialog.Clear())
                ProgressAction.MarkAll -> vm.markThrough(null)
                ProgressAction.MarkThrough -> vm.markThrough(chosen)
                null -> {}
            }
        },
        vm::dismiss,
        confirmEnabled = action != null && (action != ProgressAction.MarkThrough || until >= 0),
        busy = vm.busy,
        error = vm.dialogError,
    ) {
        val title = stringResource(R.string.content_update_progress)
        Column(Modifier.selectableGroup().semantics { contentDescription = title }) {
            for (option in ProgressAction.entries) {
                Row(
                    Modifier
                        .fillMaxWidth()
                        .selectable(option == action, enabled = !vm.busy, role = Role.RadioButton) { action = option }
                        .heightIn(min = 48.dp),
                    Arrangement.spacedBy(12.dp),
                    Alignment.CenterVertically,
                ) {
                    RadioButton(option == action, onClick = null)
                    Text(stringResource(option.label), style = MaterialTheme.typography.bodyLarge)
                }
            }
        }
        if (action == ProgressAction.MarkThrough) {
            LaunchedEffect(Unit) { vm.loadVolumes() }
            val noun = stringResource(if (volumes) R.string.update_volume else R.string.update_chapter)
            when {
                list != null -> {
                    val labels = itemLabels()
                    val value = list.getOrNull(until)?.let { itemName(it, series, labels) }
                        ?: stringResource(if (volumes) R.string.update_select_volume else R.string.update_select_chapter)
                    VButton(
                        value,
                        { picking = true },
                        // Named as the field. TalkBack reads the button's own text, the choice, after the name.
                        Modifier.fillMaxWidth().semantics { contentDescription = noun },
                        VButtonStyle.Tonal,
                        enabled = !vm.busy,
                    )
                    Text(
                        stringResource(if (volumes) R.string.update_hint_volumes else R.string.update_hint_chapters),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        style = MaterialTheme.typography.bodySmall,
                    )
                    if (picking) {
                        VolumeSheet(
                            noun,
                            list,
                            series,
                            until,
                            onPick = {
                                chosen = it
                                picking = false
                            },
                            onDismiss = { picking = false },
                        )
                    }
                }
                vm.volumesError != null -> QueryError(vm.volumesError, retry = vm::loadVolumes)
                else -> VSpinner(Modifier.align(Alignment.CenterHorizontally).size(28.dp))
            }
        }
    }
}
