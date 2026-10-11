package me.tijlvdb.voltis.ui.grid

import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.grid.LazyGridLayoutInfo
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.zIndex
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlin.math.max
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.domain.catalog.RailGeometry
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.RailSegment
import me.tijlvdb.voltis.ui.kit.VScrubber

/** Rows of overscan a window loads around the cards in view: the web's. */
private const val OVERSCAN_ROWS = 3

/** The cards in view: the grid's items of a window are its cards from index 0 on. */
private fun LazyGridLayoutInfo.firstCard() = visibleItemsInfo.firstOrNull { it.contentType == "card" }

private fun LazyGridLayoutInfo.lastCard() = visibleItemsInfo.lastOrNull { it.contentType == "card" }

/**
 * A window's side of its grid: it reports the cards in view and the first one, opens at the card to restore,
 * says when a page failed, and places the rail at the end edge down to [bottom], the space the grid keeps there.
 */
@Composable
fun BoxScope.GridRail(state: LazyGridState, items: GridItems, windowing: Windowing, columns: Int, bottom: Dp, header: () -> Int) {
    val w by rememberUpdatedState(windowing)
    val count by rememberUpdatedState(items.count)
    val snackbars = LocalSnackbars.current
    LaunchedEffect(columns) {
        snapshotFlow {
            val info = state.layoutInfo
            info.firstCard()?.let { it.index to info.lastCard()!!.index }
        }.distinctUntilChanged().collect { range ->
            if (range != null) w.onRange(max(0, range.first - OVERSCAN_ROWS * columns), range.second + OVERSCAN_ROWS * columns)
            w.onAnchor(range?.first?.takeIf { it >= columns })
        }
    }
    val restore = windowing.restoreIndex
    // A restore into a list that loaded empty has nowhere to go: it ends, and so does the anchor.
    if (restore != null && !items.loading && items.error == null && items.count == 0) {
        LaunchedEffect(Unit) {
            w.onRestored()
            w.onAnchor(null)
        }
    }
    if (restore != null && items.count > 0) {
        LaunchedEffect(Unit) {
            snapshotFlow { state.layoutInfo.totalItemsCount > 0 && header() > 0 }.first { it }
            // The header's height puts the card at the top of the screen, under none of it.
            state.scrollToItem(restore.coerceAtMost(count - 1), header())
            w.onRestored()
        }
    }
    val failed = stringResource(R.string.grid_page_failed)
    val retry = stringResource(R.string.retry)
    LaunchedEffect(windowing.failure) {
        windowing.failure?.let { failure -> snackbars.show(failed, retry, onAction = failure.retry) }
    }
    // The last row height seen, so the rail stays while only a header tall as the viewport is in view.
    var rowHeight by remember(columns) { mutableFloatStateOf(0f) }
    LaunchedEffect(columns) {
        snapshotFlow { state.layoutInfo.firstCard()?.size?.height }.filterNotNull().collect { rowHeight = it.toFloat() }
    }
    val geometry by remember(columns) {
        derivedStateOf {
            val info = state.layoutInfo
            if (rowHeight == 0f) null else RailGeometry(count, columns, rowHeight, (info.viewportSize.height - info.afterContentPadding).toFloat(), header().toFloat())
        }
    }
    val at = geometry ?: return
    if (!at.shown(windowing.segments.size)) return
    val value by remember(at) {
        derivedStateOf {
            state.layoutInfo.firstCard()?.let { at.value(it.index, -it.offset.y.toFloat()) } ?: 0f
        }
    }
    val never = stringResource(windowing.nullLabel)
    val segments = remember(at, windowing.segments, never) {
        val starts = at.starts(windowing.segments)
        windowing.segments.mapIndexed { i, s -> RailSegment(s.key?.uppercase() ?: never, starts[i]) }
    }
    val geo by rememberUpdatedState(at)
    fun seek(position: Float) {
        val seek = geo.seek(position)
        state.requestScrollToItem(seek.index, seek.offset)
    }
    VScrubber(
        segments,
        value,
        { position -> seek(position) },
        { dragging -> w.onDrag(dragging) },
        state.isScrollInProgress,
        Modifier.align(Alignment.TopEnd).zIndex(1f).fillMaxHeight().padding(top = 8.dp, bottom = bottom + 8.dp),
    )
}
