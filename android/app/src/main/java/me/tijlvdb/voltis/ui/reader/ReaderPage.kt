package me.tijlvdb.voltis.ui.reader

import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.composed
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import coil3.compose.AsyncImagePainter
import coil3.request.CachePolicy
import coil3.request.ImageRequest
import coil3.size.Precision
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.domain.comic.ClickZone
import me.tijlvdb.voltis.domain.comic.PageSource
import me.tijlvdb.voltis.domain.comic.preloadQuietly
import me.tijlvdb.voltis.domain.comic.getClickZone
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VSpinner

// What the paged and the longstrip reader share: the tap zones, and a page's loading.

/**
 * Whether the touch under way, or the last one, moved. A drag that nothing took (a swipe that may
 * not turn the page, say) still ends as a tap where the finger lifts, which the zones must ignore.
 */
internal class DragWatch {
    var dragged = false

    /** Counts the touches that turned into drags: what was aimed before one is no longer where the reader is going. */
    var drags = 0
}

/**
 * Follows every touch before the scrollers below see it, and reports to [onTap] the one case their handlers can't:
 * a stationary tap that interrupts a scroll the reader's own taps started ([scrolling], read as the finger lands).
 * That scroll runs at a priority user input can't preempt, so the touch stops nothing; the release is consumed here,
 * so a handler that sees it (a pager that takes no user scroll, an end-card button) doesn't act too. Every other tap is theirs:
 * a button, the end card or the settings panel keeps its ownership, and so does a touch an ancestor consumed.
 */
internal fun Modifier.watchDrags(
    watch: DragWatch,
    scrolling: () -> Boolean = { false },
    /** Called as a touch turns into a drag. */
    onDrag: () -> Unit = {},
    onTap: ((Offset) -> Unit)? = null,
): Modifier = composed {
    val tap by rememberUpdatedState(onTap)
    val dragging by rememberUpdatedState(onDrag)
    val busy by rememberUpdatedState(scrolling)
    pointerInput(watch) {
        awaitEachGesture {
            val down = awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
            watch.dragged = false
            // Taken before the scroller below sees the touch, which it consumes while it scrolls.
            val interrupts = !down.isConsumed && busy()
            var fingers = 1
            var last = down.position
            do {
                val event = awaitPointerEvent(PointerEventPass.Initial)
                fingers = maxOf(fingers, event.changes.count { it.pressed })
                if (!watch.dragged && event.changes.any { (it.position - down.position).getDistance() > viewConfiguration.touchSlop }) {
                    watch.dragged = true
                    watch.drags++
                    dragging()
                }
                event.changes.firstOrNull()?.let { last = it.position }
                val up = event.changes.none { it.pressed }
                if (up && interrupts && !watch.dragged && fingers == 1 && !event.changes.any { it.isConsumed } &&
                    event.changes[0].uptimeMillis - down.uptimeMillis < viewConfiguration.longPressTimeoutMillis
                ) {
                    // The one owner of this tap: the release is consumed, so nothing below acts on it too.
                    tap?.let {
                        event.changes.forEach { change -> change.consume() }
                        it(last)
                    }
                }
            } while (event.changes.any { it.pressed })
        }
    }
}

/** Acts on the zone of a tap at [at], unless the touch was a drag. */
internal fun DragWatch.tap(at: Offset, size: IntSize, centerCap: Float, flipped: Boolean, onMenu: () -> Unit, onMove: (forward: Boolean) -> Unit) {
    if (dragged) return
    zoneTap(at, size, centerCap, flipped, onMenu, onMove)
}

internal fun zoneTap(at: Offset, size: IntSize, centerCap: Float, flipped: Boolean, onMenu: () -> Unit, onMove: (forward: Boolean) -> Unit) {
    val zone = getClickZone(at.x, at.y, size.width.toFloat(), size.height.toFloat(), centerCap, flipped)
    if (zone == ClickZone.Menu) onMenu() else onMove(zone == ClickZone.Next)
}

/** What a page turn does. */
internal sealed interface PagedTurn {
    data class Go(val to: Int) : PagedTurn
    data object PreviousVolume : PagedTurn
    data object NextVolume : PagedTurn
}

/**
 * The turn a tap or button asks for. Pages run 0..[last], the end card being [last]. Another volume opens only from a
 * pager settled on the first page or the end card, and not counted from an [aimed] page: a turn that would go past an
 * edge it is only heading for stops at the edge. A tap that came down during a turn is not [settled], however still the pager is by the time it is acted on.
 */
internal fun pagedTurn(forward: Boolean, current: Int, aimed: Int?, settled: Boolean, last: Int): PagedTurn {
    val to = (aimed ?: current) + if (forward) 1 else -1
    val steady = settled && aimed == null
    return when {
        to < 0 -> if (steady && current == 0) PagedTurn.PreviousVolume else PagedTurn.Go(0)
        to > last -> if (steady && current == last) PagedTurn.NextVolume else PagedTurn.Go(last)
        else -> PagedTurn.Go(to)
    }
}

/** One page's image request, once the page is on disk, with the failure of its last attempt. */
internal class PageLoad(val request: ImageRequest?, val error: UiText?, val retry: () -> Unit)

/**
 * The page is fetched to disk first, by the one path the preloader uses, so Coil reads the file
 * and never downloads beside a preload; a fetch that failed is left to Coil, which reports it.
 * Pages skip the memory cache: decoded pages are large, and only the spreads the pager keeps
 * composed should hold one. [size] is the decode size; without one the image view decides. With
 * [exact] the bitmap is no larger than that size.
 */
@Composable
internal fun rememberPageLoad(pages: PageSource, index: Int, size: coil3.size.Size? = null, exact: Boolean = false): PageLoad {
    val context = LocalContext.current
    // By source and page: another copy's request, failure or retries are not this one's.
    var attempt by remember(pages, index) { mutableIntStateOf(0) }
    var error by remember(pages, index) { mutableStateOf<UiText?>(null) }
    // Null again in the composition that changes a key: the request of another source is never handed on.
    val request = remember(pages, index, size, exact, attempt) { mutableStateOf<ImageRequest?>(null) }
    LaunchedEffect(request) {
        pages.preloadQuietly(index)
        request.value = ImageRequest.Builder(context)
            .data(pages.page(index))
            .memoryCachePolicy(CachePolicy.DISABLED)
            // Coil and Telephoto compare requests without their listener: this makes a retry a new one.
            .memoryCacheKeyExtra("retry", attempt.toString())
            .apply { if (size != null) size(size) }
            .apply { if (exact) precision(Precision.EXACT) }
            .listener(onError = { _, result -> error = pages.failed(index, result.throwable).toUiText() })
            .build()
    }
    return PageLoad(request.value, error) {
        error = null
        attempt++
    }
}

/** Over a page that isn't showing: the spinner, or the error with Retry. */
@Composable
internal fun PageStatus(error: UiText?, onRetry: () -> Unit, modifier: Modifier = Modifier) {
    Column(
        modifier.fillMaxSize(),
        Arrangement.spacedBy(8.dp, Alignment.CenterVertically),
        Alignment.CenterHorizontally,
    ) {
        if (error == null) {
            VSpinner()
        } else {
            Text(error.resolve(), color = MaterialTheme.colorScheme.error)
            VButton(stringResource(R.string.retry), onRetry, style = VButtonStyle.Tonal)
        }
    }
}

/** A page as a plain Coil image fitted to [modifier]'s size, under its spinner or error until it shows. */
@Composable
internal fun PageLoadImage(load: PageLoad, contentDescription: String?, modifier: Modifier, alignment: Alignment = Alignment.Center) {
    var shown by remember(load.request) { mutableStateOf(false) }
    Box(modifier) {
        if (load.request != null) AsyncImage(
            model = load.request,
            contentDescription = contentDescription,
            modifier = Modifier.fillMaxSize(),
            alignment = alignment,
            contentScale = ContentScale.Fit,
            onState = { shown = it is AsyncImagePainter.State.Success },
        )
        if (!shown) PageStatus(load.error, load.retry)
    }
}
