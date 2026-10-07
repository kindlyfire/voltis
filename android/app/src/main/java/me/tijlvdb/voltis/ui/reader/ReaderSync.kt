package me.tijlvdb.voltis.ui.reader

import android.content.Context
import android.content.res.Resources
import androidx.annotation.StringRes
import androidx.activity.compose.LocalActivity
import androidx.compose.runtime.DisposableEffect
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.domain.reading.ConflictKind
import me.tijlvdb.voltis.domain.reading.LaneView
import me.tijlvdb.voltis.domain.reading.PositionLabel
import me.tijlvdb.voltis.domain.reading.PromptChoice
import me.tijlvdb.voltis.domain.reading.ReaderNotice
import me.tijlvdb.voltis.domain.reading.ReadingSession
import me.tijlvdb.voltis.domain.reading.SyncAction
import me.tijlvdb.voltis.domain.reading.SyncNotice
import me.tijlvdb.voltis.domain.reading.SyncPrompt
import me.tijlvdb.voltis.domain.reading.Undoable
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VCheckboxRow
import me.tijlvdb.voltis.ui.kit.VChoiceDialog
import me.tijlvdb.voltis.ui.kit.VNotice
import me.tijlvdb.voltis.ui.kit.VDialog
import me.tijlvdb.voltis.ui.kit.VMenuItem
import me.tijlvdb.voltis.ui.kit.VOverflowMenu
import me.tijlvdb.voltis.ui.kit.VTag
import me.tijlvdb.voltis.ui.kit.VTone
import me.tijlvdb.voltis.ui.statusLabels

// The reader's part of the reading sync: the web's ReaderSaveBanner, ReaderStatusRow, ReadingConflictModal,
// SeriesHeldModal, the clear and complete-series confirmations, and toasts.

/**
 * `ReaderSaveBanner.vue`: a conflict left for later, or saving that keeps failing. Below the top
 * bar while [chrome] shows, else below the safe top edge; it pads itself for the side insets, as
 * the reader's entry isn't padded.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SaveBanner(view: LaneView, chrome: Boolean, onReview: () -> Unit, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    val stale = view.stale
    val full = view.storageFull
    val shown = stale || full || view.failures >= 3
    // The bar's own top inset is the safe drawing one, so it is counted once.
    val top by animateDpAsState(if (chrome) TopAppBarDefaults.TopAppBarExpandedHeight + 8.dp else 8.dp, label = "bannerTop")
    if (!shown) return
    Box(
        modifier
            .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal + WindowInsetsSides.Top))
            .padding(start = 16.dp, top = top, end = 16.dp),
    ) {
        VNotice(
            stringResource(if (stale) R.string.sync_changed_elsewhere else if (full) R.string.sync_not_saved_storage_full else R.string.sync_not_saved),
            stringResource(if (stale) R.string.sync_review else R.string.retry),
            if (stale) onReview else onRetry,
            Modifier.widthIn(max = 560.dp),
            warning = !stale,
        )
    }
}

/**
 * The session's open question, as a dialog. Dismissing a conflict chooses nothing: it asks again
 * later (P2 decision 33). [title] names the item; [type] is its type, which gives the series' items
 * their noun, as on the web.
 */
@Composable
fun SyncPromptDialog(sync: ReadingSession, title: String, type: String?) {
    val prompt by sync.prompt.collectAsState()
    when (val open = prompt) {
        null -> {}
        is SyncPrompt.Conflict -> ConflictDialog(open, sync::answer, sync::dismissPrompt)
        SyncPrompt.ConfirmClear -> VDialog(
            stringResource(R.string.content_clear),
            stringResource(R.string.clear_confirm),
            { sync.answer(PromptChoice.CONFIRM) },
            sync::dismissPrompt,
            stringResource(R.string.clear_item, title),
            danger = true,
        )
        is SyncPrompt.SeriesHeld -> SeriesHeldDialog(open, title, sync::answer, sync::dismissPrompt)
        is SyncPrompt.ConfirmCompleteSeries -> CompleteSeriesDialog(open.unread, type == ContentType.BOOK, sync::answer, sync::dismissPrompt)
    }
}

/** `SeriesHeldModal.vue`: dismissing it keeps the series' status. */
@Composable
private fun SeriesHeldDialog(prompt: SyncPrompt.SeriesHeld, title: String, onChoice: (PromptChoice) -> Unit, onDismiss: () -> Unit) {
    val status = statusLabels().toMap()[prompt.status] ?: prompt.status
    val item = title.ifEmpty { stringResource(R.string.sync_this_item) }
    val text = when (prompt.item) {
        Undoable.MOVED_TO_READING -> stringResource(R.string.sync_held_moved, item, status)
        Undoable.MARKED_COMPLETED -> stringResource(R.string.sync_held_completed, item, status)
        else -> stringResource(R.string.sync_held_text, status)
    }
    VChoiceDialog(
        stringResource(if (prompt.status == ReadingStatus.DROPPED) R.string.sync_held_dropped else R.string.sync_held_on_hold),
        text,
        onDismiss,
    ) { main ->
        if (prompt.item != null) VButton(stringResource(R.string.undo), { onChoice(PromptChoice.UNDO) }, style = VButtonStyle.Text)
        VButton(stringResource(R.string.sync_held_keep, status), { onChoice(PromptChoice.KEEP) }, style = VButtonStyle.Text)
        VButton(stringResource(R.string.sync_held_move), { onChoice(PromptChoice.MOVE) }, main)
    }
}

/** `MarkSeriesCompletedModal.vue` as the reader asks it: the series' [unread] volumes are offered as a checkbox. */
@Composable
private fun CompleteSeriesDialog(unread: Int, volumes: Boolean, onChoice: (PromptChoice) -> Unit, onDismiss: () -> Unit) {
    var includeUnread by rememberSaveable { mutableStateOf(false) }
    VDialog(
        stringResource(R.string.complete_title),
        stringResource(R.string.status_mark_completed),
        { onChoice(if (includeUnread) PromptChoice.CONFIRM_WITH_UNREAD else PromptChoice.CONFIRM) },
        onDismiss,
        stringResource(if (volumes) R.string.complete_volumes else R.string.complete_chapters),
    ) {
        if (unread > 0) {
            val label = pluralStringResource(if (volumes) R.plurals.complete_include_volumes else R.plurals.complete_include_chapters, unread, unread)
            VCheckboxRow(label, includeUnread, { includeUnread = it })
        }
    }
}

/** `ReadingConflictModal.vue`, also for "Needs attention". The first button keeps what this device has. */
@Composable
fun ConflictDialog(prompt: SyncPrompt.Conflict, onChoice: (PromptChoice) -> Unit, onDismiss: () -> Unit) {
    val (titleId, keep, other) = when (prompt.kind) {
        ConflictKind.MOVED -> Triple(R.string.sync_moved_title, R.string.sync_stay to PromptChoice.STAY, R.string.sync_go to PromptChoice.GO)
        ConflictKind.COMPLETED -> Triple(R.string.sync_completed_title, R.string.sync_keep_reading to PromptChoice.KEEP, R.string.sync_reset to PromptChoice.RESET)
        ConflictKind.RESET -> Triple(R.string.sync_reset_title, R.string.sync_continue to PromptChoice.CONTINUE, R.string.sync_start to PromptChoice.START)
    }
    val text = when (prompt.kind) {
        ConflictKind.MOVED -> stringResource(R.string.sync_here_saved, prompt.here.text(), prompt.saved.text())
        ConflictKind.COMPLETED -> stringResource(R.string.sync_completed_text)
        ConflictKind.RESET -> stringResource(R.string.sync_reset_text)
    }
    VChoiceDialog(stringResource(titleId), text, onDismiss) { main ->
        VButton(stringResource(other.first), { onChoice(other.second) }, style = VButtonStyle.Text)
        VButton(stringResource(keep.first), { onChoice(keep.second) }, main)
    }
}

/** The session's notices as snackbars, with their Undo or Retry. Undo offers queue: one answer can bring two. */
@Composable
fun SyncSnackbars(sync: ReadingSession) {
    val snackbars = LocalSnackbars.current
    val context = LocalContext.current
    val statuses = statusLabels().toMap()
    val activity = LocalActivity.current
    // Their actions (a Retry of a Clear) act on this reader's lane: not once it left.
    DisposableEffect(sync) { onDispose { if (activity?.isChangingConfigurations != true) snackbars.cancelRetained() } }
    LaunchedEffect(sync) {
        sync.notices.collect { notice ->
            val (text, action) = notice.describe(context, statuses) ?: return@collect
            val label = action?.takeIf { notice.action != null }?.let(context::getString)
            snackbars.show(text, label, retained = true) { notice.action?.invoke() }
        }
    }
}

/** Its text and action label, or null for a notice the reader doesn't show. */
private fun ReaderNotice.describe(context: Context, statuses: Map<String, String>): Pair<String, Int?>? = when (val n = notice) {
    is SyncNotice.Followed -> context.getString(R.string.sync_followed, n.label.text(context.resources)) to R.string.undo
    is SyncNotice.StatusElsewhere -> when (val status = n.status) {
        null -> context.getString(R.string.sync_status_cleared_elsewhere)
        else -> context.getString(R.string.sync_status_elsewhere, statuses[status] ?: status)
    } to null
    is SyncNotice.UndoOffer -> context.getString(
        when (n.what) {
            Undoable.MOVED_TO_READING -> R.string.sync_moved_to_reading
            Undoable.MARKED_COMPLETED -> R.string.sync_marked_completed
            Undoable.SERIES_MOVED_TO_READING -> R.string.sync_series_moved
        },
    ) to R.string.undo
    is SyncNotice.NotSaved -> context.getString(R.string.error_not_saved_storage_full) to null
    is SyncNotice.Done -> when (n.what) {
        SyncAction.CLEAR -> context.getString(R.string.msg_cleared) to null
        SyncAction.MARK_SERIES_COMPLETED -> context.getString(R.string.msg_series_completed) to null
        SyncAction.UNDO, SyncAction.MOVE_SERIES -> null
    }
    is SyncNotice.Failed -> context.getString(
        when (n.what) {
            SyncAction.CLEAR -> R.string.sync_clear_failed
            SyncAction.UNDO -> R.string.sync_undo_failed
            SyncAction.MOVE_SERIES -> R.string.sync_move_series_failed
            SyncAction.MARK_SERIES_COMPLETED -> R.string.sync_complete_series_failed
        },
        n.error.toUiText().string(context),
    ) to R.string.retry
    else -> null
}

private fun PositionLabel.text(resources: Resources) = when (this) {
    is PositionLabel.Page -> resources.getString(R.string.sync_page, number)
    is PositionLabel.Percent -> resources.getString(R.string.sync_percent, value)
    PositionLabel.End -> resources.getString(R.string.sync_end)
}

@Composable
private fun PositionLabel.text() = text(LocalResources.current)

/** The status row's actions, in the web's order of conditions for the primary one. */
enum class StatusAction(@param:StringRes val label: Int) {
    TRACK(R.string.reader_track),
    RESET(R.string.sync_reset),
    RESUME_SERIES(R.string.reader_resume_series),
    COMPLETE(R.string.status_mark_completed),
    READING(R.string.reader_set_reading),
}

/** What the chip says: the status, or with "not tracking", "Series is On Hold", or "Caught up". */
enum class StatusChip { STATUS, NOT_TRACKING, SERIES_HELD, CAUGHT_UP }

data class StatusRowModel(val chip: StatusChip, val tone: VTone, val primary: StatusAction, val overflow: List<StatusAction>)

/** `ReaderStatusRow.vue`'s choices, on the shown status. */
fun statusRow(view: LaneView): StatusRowModel {
    val status = view.status
    val series = view.series
    val held = series?.status == ReadingStatus.ON_HOLD || series?.status == ReadingStatus.DROPPED
    val (chip, tone) = when {
        !view.tracking -> StatusChip.NOT_TRACKING to VTone.Neutral
        status == ReadingStatus.COMPLETED -> StatusChip.STATUS to VTone.Success
        held -> StatusChip.SERIES_HELD to VTone.Warning
        series?.caughtUp == true && series.status == ReadingStatus.READING -> StatusChip.CAUGHT_UP to VTone.Primary
        else -> StatusChip.STATUS to if (status == ReadingStatus.READING) VTone.Primary else VTone.Neutral
    }
    val primary = when {
        !view.tracking -> StatusAction.TRACK
        status == ReadingStatus.COMPLETED -> StatusAction.RESET
        held -> StatusAction.RESUME_SERIES
        else -> StatusAction.COMPLETE
    }
    val overflow = listOfNotNull(
        StatusAction.COMPLETE.takeIf { status != ReadingStatus.COMPLETED },
        StatusAction.RESUME_SERIES.takeIf { held },
        StatusAction.READING.takeIf { status != ReadingStatus.READING },
        StatusAction.RESET,
    ) - primary
    return StatusRowModel(chip, tone, primary, overflow)
}

/**
 * `ReaderStatusRow.vue`, at the top of the settings sheet: the chip, the primary action and the
 * rest in a menu. "Saved on this device" while the outbox holds this item's changes and the server
 * can't be reached.
 */
@Composable
fun ReaderStatusRow(view: LaneView, busy: Boolean, onAction: (StatusAction) -> Unit) {
    val row = statusRow(view)
    val statuses = statusLabels().toMap()
    val status = view.status?.let { statuses[it] ?: it } ?: stringResource(R.string.reader_no_status)
    val chip = when (row.chip) {
        StatusChip.STATUS -> status
        StatusChip.NOT_TRACKING -> stringResource(R.string.reader_not_tracking, status)
        StatusChip.SERIES_HELD -> stringResource(R.string.reader_series_is, view.series?.status?.let { statuses[it] ?: it }.orEmpty())
        StatusChip.CAUGHT_UP -> stringResource(R.string.card_caught_up, status)
    }
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.weight(1f)) { VTag(chip, tone = row.tone) }
            VButton(stringResource(row.primary.label), { onAction(row.primary) }, Modifier.padding(start = 8.dp), VButtonStyle.Tonal, enabled = !busy)
            VOverflowMenu(
                row.overflow.map { action -> VMenuItem(stringResource(action.label)) { onAction(action) } },
                enabled = !busy,
                label = stringResource(R.string.reader_status_more),
            )
        }
        if (view.unsent && view.parked && !view.storageFull) {
            Text(stringResource(R.string.reader_saved_here), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
        }
    }
}
