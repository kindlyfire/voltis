package me.tijlvdb.voltis.ui.reader

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.domain.catalog.itemName
import me.tijlvdb.voltis.domain.comic.Siblings
import me.tijlvdb.voltis.domain.reading.LaneView
import me.tijlvdb.voltis.domain.reading.SeriesInfo
import me.tijlvdb.voltis.ui.itemLabels
import me.tijlvdb.voltis.ui.statusLabels
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VSpinner

/**
 * Past the last page: the next volume, or the end of the series, or the siblings' own state.
 * [readablePrev] and [readableNext] are the siblings that can be opened now; a next volume that
 * can't is named, without Read next.
 */
@Composable
fun EndCard(
    content: Content,
    series: Content?,
    siblings: Siblings,
    readablePrev: Content?,
    readableNext: Content?,
    view: LaneView,
    earlierUnread: String?,
    onVolume: (String) -> Unit,
    onExit: () -> Unit,
    onRetry: () -> Unit,
    onCompleteSeries: () -> Unit,
) {
    Column(
        Modifier.fillMaxSize().safeDrawingPadding().padding(32.dp),
        Arrangement.spacedBy(16.dp, Alignment.CenterVertically),
        Alignment.CenterHorizontally,
    ) {
        val next = siblings.next
        val prev = readablePrev
        when {
            siblings.status == Siblings.Status.Loading -> VSpinner(label = stringResource(R.string.reader_finding_next))
            siblings.status == Siblings.Status.Error -> SiblingsRetry(onRetry)
            next != null -> {
                Text(
                    itemName(next, series, itemLabels()),
                    Modifier.semantics { heading() },
                    style = MaterialTheme.typography.headlineSmall,
                    textAlign = TextAlign.Center,
                )
                if (readableNext != null) {
                    VButton(stringResource(R.string.reader_read_next), { onVolume(next.id) })
                } else {
                    Text(stringResource(R.string.reader_not_downloaded), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    VButton(stringResource(if (content.parentId != null) R.string.reader_back_series else R.string.reader_back_comic), onExit, style = VButtonStyle.Tonal)
                }
            }
            else -> {
                val inSeries = content.parentId != null
                Text(
                    stringResource(if (inSeries) R.string.reader_end_series else R.string.reader_end),
                    Modifier.semantics { heading() },
                    style = MaterialTheme.typography.headlineSmall,
                    textAlign = TextAlign.Center,
                )
                EndSummary(view, earlierUnread, onVolume, onCompleteSeries)
                VButton(
                    stringResource(if (inSeries) R.string.reader_back_series else R.string.reader_back_comic),
                    onExit,
                    style = VButtonStyle.Tonal,
                )
                // "Read earlier volume" is the better way back.
                if (prev != null && earlierUnread == null) {
                    VButton(stringResource(R.string.reader_prev_volume), { onVolume(prev.id) }, style = VButtonStyle.Text)
                }
            }
        }
    }
}

/** `ReaderEndSummary.vue`: where the series stands, with an earlier unread volume and Mark series completed; else the item's status. */
@Composable
private fun EndSummary(view: LaneView, earlierUnread: String?, onVolume: (String) -> Unit, onCompleteSeries: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val series = view.series
    if (series == null) {
        val status = view.status ?: return
        Text(statusLabels().toMap()[status] ?: status, color = muted, style = MaterialTheme.typography.bodyMedium)
        return
    }
    seriesRead(series)?.let { read ->
        Text(
            if (series.caughtUp) stringResource(R.string.reader_caught_up, read) else read,
            color = muted,
            style = MaterialTheme.typography.bodyMedium,
            textAlign = TextAlign.Center,
        )
    }
    if (earlierUnread != null) {
        VButton(stringResource(R.string.reader_earlier_volume), { onVolume(earlierUnread) }, style = VButtonStyle.Text)
    }
    if (series.status != ReadingStatus.COMPLETED) {
        VButton(
            stringResource(R.string.reader_complete_series),
            onCompleteSeries,
            style = if (series.caughtUp) VButtonStyle.Filled else VButtonStyle.Tonal,
        )
    }
}

/** "3/4 read · 1 dropped"; null without volumes. */
@Composable
private fun seriesRead(series: SeriesInfo): String? {
    if (series.childrenCount <= 0) return null
    val read = stringResource(R.string.progress_read, series.completedChildrenCount, series.childrenCount)
    return if (series.droppedChildrenCount > 0) stringResource(R.string.progress_dropped, read, series.droppedChildrenCount) else read
}

/** The end of a volume whose series' other volumes failed to load. */
@Composable
fun SiblingsRetry(onRetry: () -> Unit) {
    Text(
        stringResource(R.string.reader_siblings_error),
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        style = MaterialTheme.typography.bodyMedium,
        textAlign = TextAlign.Center,
    )
    VButton(stringResource(R.string.retry), onRetry, style = VButtonStyle.Tonal)
}
