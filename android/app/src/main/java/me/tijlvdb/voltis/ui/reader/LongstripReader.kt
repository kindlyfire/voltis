package me.tijlvdb.voltis.ui.reader

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.BitmapRegionDecoder
import android.graphics.Paint
import android.graphics.Rect
import android.os.Build
import androidx.compose.foundation.background
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animate
import androidx.compose.animation.core.spring
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancelAndJoin
import androidx.compose.foundation.MutatePriority
import androidx.compose.foundation.gestures.animateScrollBy
import androidx.compose.foundation.gestures.scrollBy
import androidx.compose.ui.semantics.scrollByOffset
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.semantics.scrollBy
import androidx.compose.ui.semantics.scrollToIndex
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculateCentroid
import androidx.compose.foundation.gestures.calculatePan
import androidx.compose.foundation.gestures.calculateZoom
import android.os.SystemClock
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.drawscope.drawIntoCanvas
import androidx.compose.ui.graphics.drawscope.withTransform
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.input.nestedscroll.NestedScrollConnection
import androidx.compose.ui.input.nestedscroll.NestedScrollSource
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.layout
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import java.io.File
import kotlin.concurrent.thread
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.isActive
import kotlinx.coroutines.job
import me.tijlvdb.voltis.domain.comic.PageUnsupported
import java.io.IOException
import kotlin.math.ceil
import kotlin.math.min
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.api.attemptResult
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.domain.comic.Band
import me.tijlvdb.voltis.domain.comic.PageDimensions
import me.tijlvdb.voltis.domain.comic.PageSource
import me.tijlvdb.voltis.domain.comic.StripItem
import me.tijlvdb.voltis.domain.comic.StripLayout
import me.tijlvdb.voltis.domain.comic.layoutStrip
import me.tijlvdb.voltis.domain.comic.pageAt
import me.tijlvdb.voltis.ui.UiText

private const val MAX_ZOOM = 3f

/** The px that taps have yet to scroll, and the scroll working through them. */
private class TapScroll {
    var owed = 0f
    var job: Job? = null

    /** When a tap last added to [owed]. A touch that doesn't scroll stops nothing, but the sequence still ends with time. */
    var at = 0L

    /** The speed the running scroll had when it was last stopped, for the one that takes over. */
    var velocity = 0f

    /** Ends the sequence: nothing is owed and nothing runs, as after a placement, a new layout or a manual gesture. */
    fun reset() {
        job?.cancel()
        job = null
        owed = 0f
        at = 0L
        velocity = 0f
    }

    /** [reset], then waits for the scroll to end: it holds the list at a priority that a placement can't preempt. */
    suspend fun stop() {
        val running = job
        reset()
        running?.cancelAndJoin()
    }
}

/** The share of the viewport that a tap on a zone scrolls. */
private const val TAP_SCROLL = 0.80f

/** How long after a tap its scroll may still be owed (ms): longer than the scroll takes. */
private const val TAP_LAPSE = 900L

/** A tap's scroll takes 1.25x the default spring's time (the time goes with 1/sqrt(stiffness)). */
private const val TAP_STIFFNESS = Spring.StiffnessMedium / 1.5625f

/**
 * Longstrip mode: the pages in one scrolling column. [page] places it, anew whenever [placement]
 * changes; a page reached by the user's own scrolling or a tap zone is reported instead.
 */
@Composable
fun LongstripReader(
    pages: PageSource,
    page: Int,
    placement: Int,
    widthPercent: Int,
    /** Without a next volume the end card closes the strip. */
    hasEndCard: Boolean,
    moves: Flow<Boolean>,
    /** Where the strip was left within its page, if it is still that placement: a rotation gives the new list this place. */
    anchor: StripAnchor?,
    onAnchor: (StripAnchor?) -> Unit,
    onPageSettled: (Int) -> Unit,
    onReachedEnd: () -> Unit,
    onPreviousVolume: () -> Unit,
    onNextVolume: () -> Unit,
    onMenu: () -> Unit,
    endCard: @Composable () -> Unit,
) {
    // The whole-page fallback's bitmaps belong to this reader. Off the main thread: a decode may hold the lock.
    DisposableEffect(Unit) { onDispose { WholePages.release() } }
    val density = LocalDensity.current.density
    BoxWithConstraints(Modifier.fillMaxSize().clipToBounds()) {
        val viewport = IntSize(constraints.maxWidth, constraints.maxHeight)
        // Sizes the server didn't have, read from the pages' files as they come into view.
        val probed = remember(pages) { mutableStateMapOf<Int, PageDimensions>() }
        val sizes = pages.pages.mapIndexed { index, size -> probed[index] ?: size }
        val strip by rememberUpdatedState(
            remember(sizes, viewport, widthPercent, density) {
                layoutStrip(sizes, viewport.width, viewport.height, widthPercent, density)
            },
        )
        // One list for the pages: a width change (the settings panel, a rotation) keeps its place below.
        // Restored once, at composition start. Accepted limitation: a page whose size is neither in the source nor
        // probed yet (probed sizes don't survive a rotation) has no place inside it, so it restores to its top.
        val start = remember(pages) { anchor?.takeIf { it.placement == placement && it.page in strip.firstItem.indices } }
        val restored = remember(pages) { start?.let(strip::spotOf) }
        val list = remember(pages) {
            LazyListState(restored?.index ?: strip.firstItem.getOrElse(start?.page ?: page) { 0 }, restored?.offset ?: 0)
        }
        // The place within its page is the one thing a rotation loses: left for the next list.
        val latestStrip by rememberUpdatedState(strip)
        val latestPlacement by rememberUpdatedState(placement)
        val keep by rememberUpdatedState(onAnchor)
        DisposableEffect(list) {
            onDispose { keep(latestStrip.anchorAt(list.firstVisibleItemIndex, list.firstVisibleItemScrollOffset, latestPlacement)) }
        }
        var restoring by remember(list) { mutableStateOf(start != null) }
        // Scrolled by the user since the last placement: only then is a scroll reading.
        var armed by remember(list) { mutableStateOf(false) }
        var zoom by remember(list) { mutableFloatStateOf(1f) }
        var panX by remember(list) { mutableFloatStateOf(0f) }
        // The spot held across a new layout: found in the old one while the list still shows it.
        val held = remember(list) { Held(strip, placement) }
        val reflow = remember(strip, placement) {
            Snapshot.withoutReadObservation { held.reflowTo(strip, placement, list.firstVisibleItemIndex, list.firstVisibleItemScrollOffset) }
        }
        // How often each failed page was retried: every band of a page loads again with it.
        val retries = remember(pages) { mutableStateMapOf<Int, Int>() }
        val scope = rememberCoroutineScope()
        val centerCap = with(LocalDensity.current) { 300.dp.toPx() }
        val settled by rememberUpdatedState(onPageSettled)
        val reachedEnd by rememberUpdatedState(onReachedEnd)
        val previousVolume by rememberUpdatedState(onPreviousVolume)
        val nextVolume by rememberUpdatedState(onNextVolume)
        val menu by rememberUpdatedState(onMenu)

        // The px that taps still have to scroll. A tap during a scroll adds to it, and the scroll goes on
        // from where it is, with its speed, so quick taps chain instead of each starting over.
        val taps = remember(list) { TapScroll() }
        LaunchedEffect(list, placement) {
            taps.stop()
            armed = false
            zoom = 1f
            panX = 0f
            // The first run of a list that opened on its place has nothing to place.
            if (restoring) restoring = false else list.scrollToItem(strip.firstItem.getOrElse(page) { 0 })
        }
        // A new layout is a placement, not reading: the same place in the page, the zoom kept.
        // Whatever the page is (the end card, one without a size), it is not reading, and the pan
        // fits the new width. An explicit placement in the same change has the say on where it is.
        LaunchedEffect(reflow) {
            if (reflow == null) return@LaunchedEffect
            // Applied here, not while composing: a composition that is thrown away changes nothing.
            held.strip = strip
            held.placement = placement
            if (!reflow.laidOut) return@LaunchedEffect
            taps.stop()
            armed = false
            panX = panX.coerceIn(viewport.width * (1 - zoom), 0f)
            reflow.spot?.let { list.scrollToItem(it.index, it.offset) }
        }
        // Every layout the user scrolled or zoomed to: the page at the centre (the view model
        // ignores one it already has), then the end, once, when the last page's bottom comes into view.
        LaunchedEffect(list) {
            var atBottom = false
            snapshotFlow { list.layoutInfo }.collect { info ->
                val visible = info.visibleItemsInfo
                val last = visible.lastOrNull()
                if (!armed || last == null) {
                    atBottom = false
                    return@collect
                }
                strip.pageAt(visible.first().index, visible.map { it.offset }, info.viewportEndOffset / 2)?.let(settled)
                val lastItem = strip.items.lastIndex
                val bottom = last.index > lastItem || last.index == lastItem && last.offset + last.size <= info.viewportEndOffset
                if (bottom && !atBottom) reachedEnd()
                atBottom = bottom
            }
        }

        // Read at each step: a zoom animation or gesture running through a reflow clamps to the new width.
        val viewportWidth by rememberUpdatedState(viewport.width)
        // Scales about [at], which stays under the finger. The list scrolls instead of panning on y.
        fun zoomBy(factor: Float, at: Offset, pan: Offset = Offset.Zero) {
            val old = zoom
            zoom = (old * factor).coerceIn(1f, MAX_ZOOM)
            panX = (at.x - (at.x - panX) * zoom / old + pan.x).coerceIn(viewportWidth * (1 - zoom), 0f)
            list.dispatchRawDelta(at.y / old - at.y / zoom - pan.y / zoom)
        }
        fun tapBy(delta: Float) = with(taps) {
            if (SystemClock.uptimeMillis() - at > TAP_LAPSE) reset()
            at = SystemClock.uptimeMillis()
            owed += delta
            job?.cancel()
            job = scope.launch {
                // Not stopped by a touch: a touch-down's stop-scroll is user input, which this priority rejects.
                list.scroll(MutatePriority.PreventUserInput) {
                    var last = 0f
                    val target = owed
                    animate(0f, target, velocity, spring(stiffness = TAP_STIFFNESS)) { value, v ->
                        velocity = v
                        val step = value - last
                        last = value
                        val done = scrollBy(step)
                        owed -= done
                        // The end of the list: nothing more to scroll.
                        if (done != step) owed = 0f
                    }
                }
                owed = 0f
                velocity = 0f
            }
        }
        // [interrupt]: the tap came down on a scroll of the reader's own taps, which a tap past the end only absorbs.
        fun move(forward: Boolean, interrupt: Boolean = false) {
            // A tap that scrolls is reading.
            armed = true
            val sign = if (forward) 1 else -1
            if (SystemClock.uptimeMillis() - taps.at > TAP_LAPSE) taps.reset()
            when {
                // Still scrolling that way, so the strip is not at its end yet.
                taps.owed * sign > 0 || (if (forward) list.canScrollForward else list.canScrollBackward) ->
                    tapBy(TAP_SCROLL * list.layoutInfo.viewportSize.height * sign)
                // Another volume opens only from a strip that was already at its end, and not scrolling there.
                interrupt || taps.job?.isActive == true -> Unit
                forward -> {
                    reachedEnd()
                    nextVolume()
                }
                else -> previousVolume()
            }
        }
        LaunchedEffect(moves, list) { moves.collect { move(it) } }
        val drags = remember { DragWatch() }
        // Drags and the scroll actions of accessibility services arrive as user input.
        val arming = remember(list) {
            object : NestedScrollConnection {
                override fun onPreScroll(available: Offset, source: NestedScrollSource): Offset {
                    if (source == NestedScrollSource.UserInput) {
                        armed = true
                        // Not for the touch that stops a tap's scroll, which moves nothing: that one may be a tap too.
                        if (available.y != 0f) taps.reset()
                    }
                    return Offset.Zero
                }
            }
        }

        LazyColumn(
            Modifier
                // The scroll actions of accessibility services and the page keys would be rejected beside a tap's scroll:
                // they end it first (and are outermost, so they stand in for the list's own).
                .semantics {
                    scrollBy { _, y ->
                        scope.launch { taps.stop(); armed = true; list.animateScrollBy(y) }
                        true
                    }
                    scrollByOffset { offset ->
                        taps.stop()
                        armed = true
                        Offset(0f, list.scrollBy(offset.y))
                    }
                    scrollToIndex {
                        scope.launch { taps.stop(); armed = true; list.scrollToItem(it) }
                        true
                    }
                }
                .onPreviewKeyEvent { event ->
                    val page = when (event.key) {
                        Key.PageDown -> 1
                        Key.PageUp -> -1
                        else -> return@onPreviewKeyEvent false
                    }
                    if (event.type == KeyEventType.KeyDown) {
                        scope.launch { taps.stop(); armed = true; list.animateScrollBy((page * list.layoutInfo.viewportSize.height).toFloat()) }
                    }
                    true
                }
                // Outside the zoomed layer, so these are screen px. Zones are never flipped here.
                .watchDrags(drags, scrolling = { taps.job?.isActive == true }, onDrag = { taps.at = 0L }) { at ->
                    zoneTap(
                        at, viewport, centerCap, flipped = false,
                        // The touch stopped nothing: a centre tap leaves the scroll running.
                        menu,
                    ) { move(it, interrupt = true) }
                }
                .pointerInput(list, viewport) {
                    detectTapGestures(
                        onTap = { at ->
                            drags.tap(at, size, centerCap, flipped = false, menu) { move(it) }
                        },
                    )
                }
                // Before the list sees them: two fingers zoom, and one pans a zoomed strip sideways.
                .pointerInput(list, viewport) {
                    awaitEachGesture {
                        awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
                        // Once a second finger has touched, the gesture is a pinch to its end: the finger that
                        // lifts last pans and scrolls nothing, and isn't a tap or a drag for anything below.
                        var pinched = false
                        do {
                            val event = awaitPointerEvent(PointerEventPass.Initial)
                            if (event.changes.count { it.pressed } > 1) {
                                pinched = true
                                armed = true
                                taps.reset()
                                // About where the fingers were: the pan then brings that point to where they are.
                                zoomBy(event.calculateZoom(), event.calculateCentroid(useCurrent = false), event.calculatePan())
                                event.changes.forEach { it.consume() }
                            } else if (pinched) {
                                event.changes.forEach { it.consume() }
                            } else if (zoom > 1f) {
                                zoomBy(1f, Offset.Zero, Offset(event.calculatePan().x, 0f))
                            }
                        } while (event.changes.any { it.pressed })
                    }
                }
                .nestedScroll(arming)
                // The list is as tall as what shows of it, so its ends can be reached while zoomed.
                .layout { measurable, constraints ->
                    val height = ceil(constraints.maxHeight / zoom).toInt()
                    val placeable = measurable.measure(Constraints.fixed(constraints.maxWidth, height))
                    layout(constraints.maxWidth, constraints.maxHeight) {
                        placeable.placeWithLayer(0, 0) {
                            transformOrigin = TransformOrigin(0f, 0f)
                            scaleX = zoom
                            scaleY = zoom
                            translationX = panX
                        }
                    }
                },
            state = list,
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            // Keyed, so the strip keeps its place when a page above gets its size and its bands.
            items(strip.items.size, key = { strip.items[it].let { item -> item.page.toLong() shl 32 or (item.band?.srcTop ?: 0).toLong() } }) { index ->
                val item = strip.items[index]
                val band = item.band
                val retry = retries[item.page] ?: 0
                when {
                    item.height == 0 -> key(pages, retry) { UnsizedPage(pages, item, { retries[item.page] = retry + 1 }) { probed[item.page] = it } }
                    band == null -> key(pages) { StripPage(pages, item) }
                    // The list keeps an item across a width change: its bitmap is of the old size.
                    else -> key(pages, item, retry) { BandImage(pages, item, band) { retries[item.page] = retry + 1 } }
                }
            }
            if (hasEndCard) item(key = "end") { Box(Modifier.fillParentMaxHeight().fillMaxWidth()) { endCard() } }
        }
    }
}

/** Where in a strip layout a place is: an item, and the px into it. */
internal class Spot(val index: Int, val offset: Int)

/**
 * A place in a page of the strip, held by the view model across a rotation: the page and how far into it
 * (0 to 1), which any layout of the page can place. [placement] is the one it was taken under.
 */
class StripAnchor(val page: Int, val fraction: Double, val placement: Int)

/** The place [scrolled] px into the item [first]: its page and how far into it, or null for one without a size. */
internal fun StripLayout.anchorAt(first: Int, scrolled: Int, placement: Int): StripAnchor? {
    val item = items.getOrNull(first) ?: return null
    val items = items.withIndex().filter { it.value.page == item.page }
    val total = items.sumOf { it.value.height }
    if (total == 0) return null
    val into = items.takeWhile { it.index < first }.sumOf { it.value.height } + scrolled
    return StripAnchor(item.page, into.toDouble() / total, placement)
}

/** Where [anchor] is in this layout, or null when its page has no height here (its size isn't known yet). */
internal fun StripLayout.spotOf(anchor: StripAnchor): Spot? {
    val target = items.withIndex().filter { it.value.page == anchor.page }
    if (target.sumOf { it.value.height } == 0) return null
    var left = (anchor.fraction * target.sumOf { it.value.height }).toInt()
    for ((index, new) in target) {
        if (left < new.height || index == target.last().index) return Spot(index, left.coerceAtMost(new.height))
        left -= new.height
    }
    return null
}

/** A new layout or placement: [spot] is where the old layout's place is in the new one, when known. */
private class Reflow(val laidOut: Boolean, val spot: Spot?)

/** The layout and placement the list currently shows. */
private class Held(var strip: StripLayout, var placement: Int) {
    /**
     * Null when nothing is new. Changes nothing here; the caller sets [strip] and [placement] once
     * it applies the reflow. [Reflow.spot] is null when a placement came with the layout.
     */
    fun reflowTo(next: StripLayout, nextPlacement: Int, first: Int, scrolled: Int): Reflow? {
        val laidOut = next !== strip
        if (!laidOut && nextPlacement == placement) return null
        val spot = if (laidOut && nextPlacement == placement) strip.anchorAt(first, scrolled, placement)?.let(next::spotOf) else null
        return Reflow(laidOut, spot)
    }
}

/** An exact size in px: through dp a band could come out a pixel off, which would show as a seam. */
private fun Modifier.sizePx(width: Int, height: Int) = layout { measurable, _ ->
    val placeable = measurable.measure(Constraints.fixed(width, height))
    layout(width, height) { placeable.place(0, 0) }
}

@Composable
private fun placeholder() = MaterialTheme.colorScheme.onSurface.copy(alpha = 0.1f)

/** A whole page, decoded at the item's size. */
@Composable
private fun StripPage(pages: PageSource, item: StripItem) {
    PageLoadImage(
        rememberPageLoad(pages, item.page, coil3.size.Size(item.width, item.height), exact = true),
        stringResource(R.string.reader_page, item.page + 1),
        Modifier.sizePx(item.width, item.height).background(placeholder()),
    )
}

/**
 * A page whose size the server didn't have. Its file gives it, before anything is decoded, and
 * the strip then lays the page out like any other: in bands, when it is very tall.
 */
@Composable
private fun UnsizedPage(pages: PageSource, item: StripItem, onRetry: () -> Unit, onSize: (PageDimensions) -> Unit) {
    var error by remember { mutableStateOf<UiText?>(null) }
    LaunchedEffect(Unit) { error = attempt { onSize(pages.read(item.page, ::imageSize)) } }
    Box(Modifier.width(with(LocalDensity.current) { item.width.toDp() }).height(200.dp).background(placeholder())) {
        PageStatus(error, onRetry)
    }
}

private fun imageSize(file: File): PageDimensions {
    val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
    BitmapFactory.decodeFile(file.path, bounds)
    // Not an IOException: the file is here, so this isn't the network's fault.
    check(bounds.outWidth > 0 && bounds.outHeight > 0) { "Can't read the page's size" }
    return PageDimensions(bounds.outWidth, bounds.outHeight)
}

/**
 * One band of a very tall page. It holds only its own rows, decoded from the page's file, and
 * lets them go when it leaves the list's composition. The first band names the page.
 */
@Composable
private fun BandImage(pages: PageSource, item: StripItem, band: Band, onRetry: () -> Unit) {
    var bitmap by remember { mutableStateOf<Bitmap?>(null) }
    var error by remember { mutableStateOf<UiText?>(null) }
    LaunchedEffect(Unit) {
        // Held here until the state has it: a band that left mid-decode still gets it back, to recycle.
        var decoded: Bitmap? = null
        try {
            error = attemptResult {
                val job = currentCoroutineContext().job
                pages.read(item.page) { decoded = decodeBand(it, band) { job.isActive } }
                bitmap = decoded
                decoded = null
            }.exceptionOrNull()?.let { pages.failed(item.page, it).toUiText() }
        } finally {
            decoded?.recycle()
        }
    }
    DisposableEffect(Unit) { onDispose { bitmap?.recycle() } }

    val label = stringResource(R.string.reader_page, item.page + 1)
    val named = if (band.srcTop == 0) Modifier.semantics { contentDescription = label } else Modifier
    Box(Modifier.sizePx(item.width, item.height).then(named)) {
        val shown = bitmap
        if (shown != null) {
            // The page's one scale and this band's offset: the item's bounds cut off the overlap.
            Box(
                Modifier.fillMaxSize().drawBehind {
                    withTransform({
                        clipRect()
                        translate(top = band.offset)
                        scale(size.width / shown.width, band.scale, Offset.Zero)
                    }) { drawIntoCanvas { it.nativeCanvas.drawBitmap(shown, 0f, 0f, BandPaint) } }
                },
            )
        } else {
            PageStatus(error, onRetry, Modifier.background(placeholder()))
        }
    }
}

// Not anti-aliased: while zoomed, the blended edges of two bands would show as a line between them.
private val BandPaint = Paint(Paint.FILTER_BITMAP_FLAG)

/** The rows of [band], sampled as `layoutStrip` set. Only those rows are decoded, unless the format has no region decoder. */
private fun decodeBand(file: File, band: Band, active: () -> Boolean): Bitmap {
    val decoder = try {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            BitmapRegionDecoder.newInstance(file.path)
        } else {
            @Suppress("DEPRECATION")
            BitmapRegionDecoder.newInstance(file.path, false)
        }
    } catch (e: IOException) {
        // A format it can't cut regions from (GIF, AVIF): the page is decoded whole instead.
        return WholePages.band(file, band, active)
    }
    try {
        val options = BitmapFactory.Options().apply { inSampleSize = band.sample }
        val region = Rect(0, band.srcTop, decoder.width, min(band.srcBottom, decoder.height))
        decoder.decodeRegion(region, options)?.let { return it }
    } catch (e: IllegalArgumentException) {
        // AVIF opens but can't be cut ("invalid input").
    } finally {
        decoder.recycle()
    }
    return WholePages.band(file, band, active)
}

/**
 * The fallback for formats BitmapRegionDecoder can't cut (GIF, AVIF): a page is decoded whole
 * once, at its bands' sampling, and each band is copied from that. One decode runs at a time and
 * the last [KEPT] pages stay, so the memory is bounded however many bands are composed.
 */
private object WholePages {
    /** The most pixels one whole decode may take (about 200 MB as ARGB_8888). */
    private const val MAX_PIXELS = 50_000_000L
    private const val KEPT = 2

    private class Key(val path: String, val sample: Int) {
        override fun equals(other: Any?) = other is Key && other.path == path && other.sample == sample
        override fun hashCode() = path.hashCode() * 31 + sample
    }

    // Access-ordered: the eldest is the least recently used. Guarded by the object's lock.
    private val cache = LinkedHashMap<Key, Bitmap>(4, 0.75f, true)

    /** Recycles what is kept, on a background thread: a decode under way holds the lock. */
    fun release() {
        thread(name = "wholepages-release") { synchronized(this) { cache.values.forEach { it.recycle() }; cache.clear() } }
    }

    @Synchronized
    fun band(file: File, band: Band, active: () -> Boolean): Bitmap {
        val sample = band.sample.coerceAtLeast(1)
        if (!active()) throw CancellationException()
        val key = Key(file.path, sample)
        val whole = cache[key] ?: decode(file, sample, key, active)
        val top = (band.srcTop / sample).coerceAtMost(whole.height - 1)
        val bottom = (((band.srcBottom + sample - 1) / sample)).coerceIn(top + 1, whole.height)
        try {
            // Always a copy: the band recycles its bitmap, and createBitmap returns the source for the full rectangle.
            return if (top == 0 && bottom == whole.height) {
                whole.copy(whole.config ?: Bitmap.Config.ARGB_8888, false) ?: throw PageUnsupported()
            } else {
                Bitmap.createBitmap(whole, 0, top, whole.width, bottom - top)
            }
        } catch (e: OutOfMemoryError) {
            throw PageUnsupported(e)
        }
    }

    private fun decode(file: File, sample: Int, key: Key, active: () -> Boolean): Bitmap {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeFile(file.path, bounds)
        if (bounds.outWidth <= 0 || bounds.outHeight <= 0) throw PageUnsupported()
        if (bounds.outWidth.toLong() / sample * (bounds.outHeight / sample) > MAX_PIXELS) throw PageUnsupported()
        // Room first: the pages kept go before a new one is allocated.
        while (cache.size >= KEPT) cache.remove(cache.keys.first())?.recycle()
        val whole = try {
            BitmapFactory.decodeFile(file.path, BitmapFactory.Options().apply { inSampleSize = sample })
        } catch (e: OutOfMemoryError) {
            throw PageUnsupported(e)
        } ?: throw PageUnsupported()
        if (!active()) {
            whole.recycle()
            throw CancellationException()
        }
        cache[key] = whole
        return whole
    }
}
