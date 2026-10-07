package me.tijlvdb.voltis.ui.grid

import androidx.compose.foundation.background
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.data.reading.BulkState
import me.tijlvdb.voltis.ui.downloads.fileSize
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VCheckboxRow
import me.tijlvdb.voltis.ui.kit.VDialog
import me.tijlvdb.voltis.ui.kit.VSelect
import me.tijlvdb.voltis.ui.statusLabels
import me.tijlvdb.voltis.ui.theme.VoltisShapes

/** `BulkStatusModal.vue`. While [running] it shows the batch's progress and Hide, and can't be dismissed. */
@Composable
fun BulkStatusDialog(
    count: Int,
    mayHaveSeries: Boolean,
    running: BulkState?,
    onSave: (status: String?, includeUnread: Boolean) -> Unit,
    onHide: () -> Unit,
    onDismiss: () -> Unit,
) {
    var status by rememberSaveable { mutableStateOf<String?>(null) }
    var includeUnread by rememberSaveable { mutableStateOf(false) }
    BulkDialog(
        stringResource(R.string.bulk_status_title),
        stringResource(R.string.save),
        { onSave(status, includeUnread) },
        running,
        onHide,
        onDismiss,
        text = pluralStringResource(R.plurals.bulk_items_selected, count, count),
    ) {
        VSelect(
            stringResource(R.string.bulk_status),
            statusLabels(),
            status,
            { status = it },
            clearLabel = stringResource(R.string.status_clear),
            onClear = { status = null },
            placeholder = stringResource(R.string.bulk_no_status),
            enabled = running == null,
        )
        if (mayHaveSeries && status == ReadingStatus.COMPLETED) {
            VCheckboxRow(stringResource(R.string.bulk_include_unread), includeUnread, { includeUnread = it }, enabled = running == null)
        }
    }
}

/** `BulkResetProgressModal.vue`: the titles are listed when there are at most 50. */
@Composable
fun BulkClearDialog(titles: List<String>, running: BulkState?, onClear: () -> Unit, onHide: () -> Unit, onDismiss: () -> Unit) {
    BulkDialog(
        stringResource(R.string.content_clear),
        stringResource(R.string.bulk_clear),
        onClear,
        running,
        onHide,
        onDismiss,
        text = pluralStringResource(R.plurals.bulk_clear_text, titles.size, titles.size, if (titles.size <= MAX_TITLES) ":" else "."),
        danger = true,
    ) {
        if (titles.size <= MAX_TITLES) {
            val label = stringResource(R.string.bulk_selected_titles)
            Column(
                Modifier
                    .fillMaxWidth()
                    .heightIn(max = 240.dp)
                    .background(MaterialTheme.colorScheme.surfaceContainer, VoltisShapes.field)
                    .semantics { contentDescription = label }
                    .focusable()
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 16.dp, vertical = 4.dp),
            ) {
                titles.forEachIndexed { i, title ->
                    if (i > 0) HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                    Text(title, Modifier.padding(vertical = 8.dp), maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
}

private const val MAX_TITLES = 50

/** Free space kept back from a bulk download's estimate, as the transfer keeps it per page (`Transfer.SPARE_BYTES`). */
private const val SPARE = 50L shl 20

/**
 * P4 §6's download dialog: counts the volumes behind the selection, then offers them. Sizes are
 * estimates, so a sum above the free space only warns. [onDownload] is for a plan with something to queue.
 */
@Composable
fun BulkDownloadDialog(count: DownloadCount, onRetry: () -> Unit, onDownload: () -> Unit, onDismiss: () -> Unit) {
    val plan = (count as? DownloadCount.Ready)?.plan
    val nothing = plan != null && plan.count == 0
    val live = Modifier.semantics { liveRegion = LiveRegionMode.Polite }
    VDialog(
        stringResource(R.string.bulk_download_title),
        stringResource(if (nothing) R.string.close else R.string.downloads_download),
        if (nothing) onDismiss else onDownload,
        onDismiss,
        confirmEnabled = plan != null && (nothing || !plan.tooMany),
        dismissLabel = if (nothing) null else stringResource(R.string.cancel),
    ) {
        when (count) {
            DownloadCount.Counting -> Text(stringResource(R.string.bulk_download_counting), live)
            is DownloadCount.Failed -> QueryError(count.text, retry = onRetry)
            is DownloadCount.Ready -> Column(live, Arrangement.spacedBy(12.dp)) {
                val plan = count.plan
                val muted = MaterialTheme.colorScheme.onSurfaceVariant
                when {
                    nothing -> Text(stringResource(R.string.bulk_download_nothing))
                    plan.tooMany -> Text(pluralStringResource(R.plurals.bulk_download_too_many, plan.count, plan.count))
                    else -> {
                        val size = if (plan.bytes == 0L && plan.sizeUnknown) null else fileSize(plan.bytes)
                        Text(
                            when {
                                size == null -> pluralStringResource(R.plurals.bulk_download_count, plan.count, plan.count)
                                plan.sizeUnknown -> pluralStringResource(R.plurals.bulk_download_summary_min, plan.count, plan.count, size)
                                else -> pluralStringResource(R.plurals.bulk_download_summary, plan.count, plan.count, size)
                            },
                        )
                    }
                }
                if (plan.already > 0) Text(pluralStringResource(R.plurals.bulk_download_already, plan.already, plan.already), color = muted)
                if (plan.books > 0) Text(pluralStringResource(R.plurals.bulk_download_books, plan.books, plan.books), color = muted)
                if (!nothing && !plan.tooMany) {
                    if (count.waitsForWifi) Text(stringResource(R.string.bulk_download_wifi), color = muted)
                    val free = count.free
                    if (free != null && plan.bytes > free - SPARE) {
                        Text(stringResource(R.string.bulk_download_low_space, fileSize(free)), color = MaterialTheme.colorScheme.error)
                    }
                }
            }
        }
    }
}

/** The frame of both: [confirm] starts the batch; while it runs, the progress line and Hide instead. */
@Composable
private fun BulkDialog(
    title: String,
    confirm: String,
    onConfirm: () -> Unit,
    running: BulkState?,
    onHide: () -> Unit,
    onDismiss: () -> Unit,
    text: String,
    danger: Boolean = false,
    content: @Composable () -> Unit,
) {
    VDialog(
        title,
        if (running == null) confirm else stringResource(R.string.bulk_hide),
        if (running == null) onConfirm else onHide,
        onDismiss,
        text = text,
        danger = danger && running == null,
        dismissable = running == null,
        dismissLabel = if (running == null) stringResource(R.string.cancel) else null,
    ) {
        content()
        if (running != null) Progress(running)
    }
}

/** "Saving… 12 of 40", or "Saving…" once every command ran and failures are still being stored. Its live region speaks a throttled copy of the line. */
@Composable
private fun Progress(state: BulkState) {
    val line = if (state.saving) stringResource(R.string.bulk_saving) else stringResource(R.string.bulk_progress, state.finished, state.total)
    val latest by rememberUpdatedState(line)
    var spoken by remember { mutableStateOf(line) }
    // Restarts when Saving begins, so that is spoken at once; the counts at most every few seconds.
    LaunchedEffect(state.saving) {
        while (true) {
            spoken = latest
            delay(SPEAK_EVERY)
        }
    }
    // The visible line is the live region: a node with no size is dropped from the accessibility tree.
    Text(
        line,
        Modifier.semantics {
            contentDescription = spoken
            liveRegion = LiveRegionMode.Polite
        },
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

private const val SPEAK_EVERY = 3_000L
