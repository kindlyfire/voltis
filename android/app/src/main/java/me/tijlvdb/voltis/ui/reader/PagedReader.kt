package me.tijlvdb.voltis.ui.reader

import android.os.SystemClock
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.requiredSize
import androidx.compose.foundation.layout.width
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animate
import androidx.compose.animation.core.spring
import androidx.compose.foundation.MutatePriority
import androidx.compose.foundation.gestures.animateScrollBy
import androidx.compose.foundation.gestures.scrollBy
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.isAltPressed
import androidx.compose.ui.input.key.isCtrlPressed
import androidx.compose.ui.input.key.isMetaPressed
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.semantics.scrollByOffset
import androidx.compose.ui.semantics.scrollToIndex
import androidx.compose.ui.semantics.pageLeft
import androidx.compose.ui.semantics.pageRight
import androidx.compose.ui.semantics.scrollBy
import kotlin.math.abs
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.PagerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.ScaleFactor
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.toSize
import coil3.compose.AsyncImage
import coil3.compose.AsyncImagePainter
import coil3.request.ImageRequest
import kotlin.math.roundToInt
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.job
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch
import me.saket.telephoto.ExperimentalTelephotoApi
import me.saket.telephoto.zoomable.Viewport
import me.saket.telephoto.zoomable.ZoomSpec
import me.saket.telephoto.zoomable.ZoomableContentLocation
import me.saket.telephoto.zoomable.ZoomableState
import me.saket.telephoto.zoomable.coil3.ZoomableAsyncImage
import me.saket.telephoto.zoomable.rememberZoomableImageState
import me.saket.telephoto.zoomable.rememberZoomableState
import me.saket.telephoto.zoomable.spatial.CoordinateSpace
import me.saket.telephoto.zoomable.zoomable
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.domain.comic.Extent
import me.tijlvdb.voltis.domain.comic.Fit
import me.tijlvdb.voltis.domain.comic.PageDimensions
import me.tijlvdb.voltis.domain.comic.PageSource
import me.tijlvdb.voltis.domain.comic.ReaderSettings
import me.tijlvdb.voltis.domain.comic.ReadingDirection
import me.tijlvdb.voltis.domain.comic.buildSpreads
import me.tijlvdb.voltis.domain.comic.layoutSpread
import me.tijlvdb.voltis.domain.comic.scrollStep
import me.tijlvdb.voltis.domain.comic.resolveDouble
import me.tijlvdb.voltis.domain.comic.spreadDecodeSize
import me.tijlvdb.voltis.domain.comic.spreadOfPages
import me.tijlvdb.voltis.domain.comic.spreadSharp
import me.tijlvdb.voltis.domain.comic.swipeTurns

private const val MAX_ZOOM = 3f

/** The base size of a spread: larger than the viewport for a fit that overflows, so it pans. */
/** How long a page turn takes at most: a tap after it counts from the page the pager shows. */
private const val TURN_MILLIS = 700L

// Data classes: Telephoto re-places the page when it gets a scale or alignment that isn't equal.
private data class SpreadScale(private val fit: Fit, private val zoomWide: Boolean) : ContentScale {
    override fun computeScaleFactor(srcSize: Size, dstSize: Size): ScaleFactor {
        val layout = layoutSpread(
            listOf(PageDimensions(srcSize.width.roundToInt(), srcSize.height.roundToInt())),
            Extent(dstSize.width.toDouble(), dstSize.height.toDouble()),
            fit,
            zoomWide,
        ) ?: return ScaleFactor(1f, 1f)
        val scale = (layout.height / srcSize.height).toFloat()
        return ScaleFactor(scale, scale)
    }
}

/**
 * Centres an axis that fits. An overflowing axis shows its reading-order start (the right, in
 * RTL), or its end after a backward turn.
 */
internal data class EnterAlignment(private val atEnd: Boolean, private val rtl: Boolean) : Alignment {
    override fun align(size: IntSize, space: IntSize, layoutDirection: LayoutDirection): IntOffset {
        fun axis(size: Int, space: Int, startIsFar: Boolean) = when {
            size <= space -> (space - size) / 2
            atEnd != startIsFar -> space - size
            else -> 0
        }
        return IntOffset(axis(size.width, space.width, rtl), axis(size.height, space.height, false))
    }
}

/** The spread the pager last came to rest on, and whether a backward turn brought it there. */
private data class Rest(val index: Int, val back: Boolean)

/**
 * Paged mode: one pager page per spread, and the end card after the last. [page] and [atEnd]
 * place it, anew whenever [placement] changes; a spread reached by the user's own swipe or tap
 * is reported instead.
 */
@Composable
fun PagedReader(
    pages: PageSource,
    page: Int,
    atEnd: Boolean,
    placement: Int,
    settings: ReaderSettings,
    shifted: Boolean,
    direction: ReadingDirection,
    flipped: Boolean,
    /** Previous (false) and next (true) from the chrome's buttons: what a tap on a zone does. */
    moves: Flow<Boolean>,
    onPageSettled: (Int) -> Unit,
    onReachedEnd: () -> Unit,
    onPreviousVolume: () -> Unit,
    onNextVolume: () -> Unit,
    onMenu: () -> Unit,
    endCard: @Composable () -> Unit,
) {
    val layoutDirection = LocalLayoutDirection.current
    BoxWithConstraints(Modifier.fillMaxSize()) {
        val viewport = IntSize(constraints.maxWidth, constraints.maxHeight)
        val double = resolveDouble(settings.spread, Extent(viewport.width.toDouble(), viewport.height.toDouble()))
        val spreads = remember(pages, double, shifted) { buildSpreads(pages.pages, double, shifted) }
        val spreadOf = remember(spreads) { spreadOfPages(spreads, pages.pages.size) }
        val target = if (atEnd) spreads.size else spreadOf.getOrElse(page) { spreads.size }
        // A new pager per layout, so a rotation or a spread change starts on the right spread.
        val pager = remember(spreads) { PagerState(target) { spreads.size + 1 } }
        var rest by remember(pager) { mutableStateOf(Rest(target, back = false)) }
        val rtl = direction == ReadingDirection.Rtl
        val scope = rememberCoroutineScope()
        val centerCap = with(LocalDensity.current) { 300.dp.toPx() }
        val settled by rememberUpdatedState(onPageSettled)
        val reachedEnd by rememberUpdatedState(onReachedEnd)

        // The zoom state of each composed spread, by pager index.
        val zooms = remember(pager) { mutableStateMapOf<Int, ZoomableState>() }

        val drags = remember { DragWatch() }
        // Where the turns under way end. Only for consecutive tap or button turns: a placement or a drag ends it. A quick second tap counts from where the first aimed,
        // not from where the pager is.
        val aimed = remember(pager) { intArrayOf(0) }
        val turnJob = remember(pager) { arrayOfNulls<Job>(1) }
        val aimedAt = remember(pager) { longArrayOf(0) }
        val aimedDrags = remember(pager) { intArrayOf(0) }
        // A placement (the slider, a settings change): it moves `rest` first, so the collector below
        // reports nothing. The spreads get new zoom states with it, so the placed one shows from its
        // start, also when it is the one already showing.
        var placed by remember(pager) { mutableIntStateOf(placement) }
        // The speed the last turn had when a newer one took over, for the one that takes over.
        val velocity = remember(pager) { floatArrayOf(0f) }
        // Ends the tap sequence (its aim and speed) and the turn under way, and waits for it: user input
        // (accessibility actions, keys) and placements can't run beside it. A consecutive turn doesn't come through here.
        suspend fun stopTurn() {
            aimedAt[0] = 0
            turnJob[0]?.cancelAndJoin()
            turnJob[0] = null
            velocity[0] = 0f
        }
        LaunchedEffect(pager, placement) {
            if (placement == placed) return@LaunchedEffect
            placed = placement
            rest = Rest(target, back = false)
            aimedAt[0] = 0
            // A turn runs at a priority that a placement can't preempt. The sequence ends: its speed goes with it.
            stopTurn()
            pager.scrollToPage(target)
        }
        LaunchedEffect(pager) {
            snapshotFlow { pager.settledPage }.collect { index ->
                if (index == rest.index) return@collect
                rest = Rest(index, back = index < rest.index)
                if (index == spreads.size) reachedEnd() else settled(spreads[index].first())
            }
        }

        suspend fun turn(forward: Boolean, interrupt: Boolean) {
            val recent = SystemClock.uptimeMillis() - aimedAt[0] < TURN_MILLIS && aimedDrags[0] == drags.drags
            // Speed carries only across consecutive turns.
            if (!recent) velocity[0] = 0f
            when (val turn = pagedTurn(forward, pager.currentPage, aimed[0].takeIf { recent }, !pager.isScrollInProgress && !interrupt, spreads.size)) {
                PagedTurn.PreviousVolume -> onPreviousVolume()
                PagedTurn.NextVolume -> onNextVolume()
                is PagedTurn.Go -> {
                    aimed[0] = turn.to
                    aimedAt[0] = SystemClock.uptimeMillis()
                    aimedDrags[0] = drags.drags
                    turnJob[0] = currentCoroutineContext().job
                    pager.turnTo(turn.to, velocity)
                }
            }
        }
        // A spread that doesn't fit is read through before turning.
        suspend fun move(forward: Boolean, zoom: ZoomableState?, interrupt: Boolean = false) {
            if (zoom?.step(forward, rtl) != true) turn(forward, interrupt)
        }
        // The pages get one handler that reads the zones as they are now: Telephoto keeps the
        // first click handler it is given, so a new lambda would never reach a composed page.
        val currentTap by rememberUpdatedState { at: Offset, zoom: ZoomableState? ->
            drags.tap(at, viewport, centerCap, flipped, onMenu) { scope.launch { move(it, zoom) } }
        }
        val tap = remember { { at: Offset, zoom: ZoomableState? -> currentTap(at, zoom) } }
        val onMove by rememberUpdatedState<suspend (Boolean, Boolean) -> Unit> { forward, interrupt -> move(forward, zooms[pager.currentPage], interrupt) }
        // The touch stopped nothing (a turn runs at a priority that rejects user input), so a centre tap leaves
        // the turn running. A tap that landed on a turn never opens another volume, however settled the pager is by now.
        val touchTap = { at: Offset ->
            zoneTap(at, viewport, centerCap, flipped, onMenu) { scope.launch { onMove(it, true) } }
        }
        LaunchedEffect(moves) { moves.collectLatest { onMove(it, false) } }
        // Telephoto hands a swipe to the pager at the physical edge; the reading edge is checked here.
        val swipes by remember(pager, viewport, flipped, rtl) {
            derivedStateOf {
                val bounds = zooms[pager.currentPage]?.bounds()
                val atLeft = bounds == null || bounds.left >= -1
                val atRight = bounds == null || bounds.right <= viewport.width + 1
                swipeTurns(true, flipped, rtl, atLeft, atRight) || swipeTurns(false, flipped, rtl, atLeft, atRight)
            }
        }

        // The pager and the spreads are physical: only the reading direction mirrors them.
        CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Ltr) {
            key(pager) {
                HorizontalPager(
                    pager,
                    Modifier
                        // The scroll actions of accessibility services would be rejected beside a turn: they end it first.
                        // Only where the pager takes user scroll, as its own do.
                        .semantics {
                            if (swipes) {
                                scrollBy { x, _ ->
                                    scope.launch { stopTurn(); pager.animateScrollBy(x) }
                                    true
                                }
                                scrollByOffset { offset ->
                                    stopTurn()
                                    Offset(pager.scrollBy(offset.x), 0f)
                                }
                                scrollToIndex {
                                    scope.launch { stopTurn(); pager.scrollToPage(it.coerceIn(0, spreads.size)) }
                                    true
                                }
                                // In index space, as the pager's own: the accessibility delegate mirrors for a reversed layout.
                                pageLeft {
                                    scope.launch { stopTurn(); pager.animateScrollToPage((pager.currentPage - 1).coerceAtLeast(0)) }
                                    true
                                }
                                pageRight {
                                    scope.launch { stopTurn(); pager.animateScrollToPage((pager.currentPage + 1).coerceAtMost(spreads.size)) }
                                    true
                                }
                            }
                        }
                        // Page Up and Down page in reading order, as the pager's own keys would; they end a turn first.
                        .onPreviewKeyEvent { event ->
                            val page = when (event.key) {
                                Key.PageDown -> 1
                                Key.PageUp -> -1
                                else -> return@onPreviewKeyEvent false
                            }
                            if (!swipes || event.isCtrlPressed || event.isAltPressed || event.isMetaPressed) return@onPreviewKeyEvent false
                            if (event.type == KeyEventType.KeyDown) {
                                scope.launch { stopTurn(); pager.animateScrollToPage((pager.currentPage + page).coerceIn(0, spreads.size)) }
                            }
                            true
                        }
                        .watchDrags(
                        drags,
                        // Only a pager that takes user scroll swallows the touch; and only the tap turn's own motion counts,
                        // not the settling of a swipe (a drag since the aim ends it).
                        scrolling = { swipes && SystemClock.uptimeMillis() - aimedAt[0] < TURN_MILLIS && aimedDrags[0] == drags.drags && pager.isScrollInProgress },
                        onTap = touchTap,
                    ),
                    reverseLayout = flipped,
                    userScrollEnabled = swipes,
                    beyondViewportPageCount = 1,
                    key = { spreads.getOrNull(it)?.first() ?: -1 },
                ) { index ->
                    val spread = spreads.getOrNull(index)
                    val alignment = EnterAlignment(atEnd = index < rest.index || index == rest.index && rest.back, rtl)
                    Box(Modifier.fillMaxSize().clipToBounds()) {
                        if (spread == null) {
                            Box(Modifier.fillMaxSize().onTaps { tap(it, null) }) {
                                CompositionLocalProvider(LocalLayoutDirection provides layoutDirection, endCard)
                            }
                            return@Box
                        }
                        // Telephoto's resetZoom keeps the pan of a spread that still overflows, and its
                        // node doesn't take a new state: a placement composes the spread anew.
                        key(placement) {
                            val zoom = rememberZoomableState(ZoomSpec(maxZoomFactor = MAX_ZOOM))
                            DisposableEffect(zoom) {
                                zooms[index] = zoom
                                onDispose { zooms.remove(index) }
                            }
                            if (spread.size == 1) {
                                SinglePage(pages, spread[0], zoom, SpreadScale(settings.fit, settings.zoomWide), alignment, tap)
                            } else {
                                DoublePage(pages, spread, zoom, viewport, settings.fit, pager.settledPage == index, alignment, rtl, tap)
                            }
                        }
                    }
                }
            }
        }
    }
}

/** The px that move a pager showing page position [at] (page + offset fraction) to [pos], with [step] px (page size and spacing) to a page. */
internal fun turnDelta(pos: Float, at: Float, step: Int): Float = (pos - at) * step

/**
 * Turns to [page], landing exactly on it. Runs as a scroll that user input can't preempt: a touch-down's stop-scroll
 * is rejected, so a second tap or a hold doesn't pause the turn. A newer turn takes over from where this one is, with
 * its speed (pages/s) kept in [velocity].
 * The turn is in pages, not px, so a page size that changes under it (a rotation) doesn't throw it off: each frame
 * moves by the live [PagerState.layoutInfo] step, and what the pager consumed is what counts as moved.
 */
private suspend fun PagerState.turnTo(page: Int, velocity: FloatArray) {
    scroll(MutatePriority.PreventUserInput) {
        fun step() = layoutInfo.pageSize + layoutInfo.pageSpacing
        var at = currentPage + currentPageOffsetFraction
        animate(at, page.toFloat(), velocity[0], spring(stiffness = Spring.StiffnessMediumLow)) { pos, v ->
            velocity[0] = v
            val size = step()
            if (size > 0) at += scrollBy(turnDelta(pos, at, size)) / size
        }
        // Exactly on the page, offset 0, in this same scroll.
        step().takeIf { it > 0 }?.let { size -> at += scrollBy(turnDelta(page.toFloat(), at, size)) / size }
        val rest = turnDelta(page.toFloat(), currentPage + currentPageOffsetFraction, step())
        if (abs(rest) > 0.5f) scrollBy(rest)
        velocity[0] = 0f
    }
}

/** Taps for the first [onTap] it is given, which must not go stale. */
private fun Modifier.onTaps(onTap: (Offset) -> Unit) = pointerInput(Unit) { detectTapGestures { onTap(it) } }

/** Where the spread is, in viewport px: beyond the viewport on an axis it overflows. */
@OptIn(ExperimentalTelephotoApi::class)
private fun ZoomableState.bounds(): Rect =
    with(coordinateSystem) { contentBounds(clipToViewport = false).rectIn(CoordinateSpace.Viewport) }

/**
 * Moves on within a spread that overflows the viewport, as `scroller.step` in
 * `ReaderModePaged.vue`. False at its edge, or when it fits: the move turns the page.
 */
@OptIn(ExperimentalTelephotoApi::class)
private suspend fun ZoomableState.step(forward: Boolean, rtl: Boolean): Boolean {
    val viewport = coordinateSystem.viewportSize
    val bounds = bounds()
    val horizontal = bounds.width > viewport.width + 1
    if (!horizontal && bounds.height <= viewport.height + 1) return false
    val max = if (horizontal) bounds.width - viewport.width else bounds.height - viewport.height
    // The distance from the reading-order start, mirrored on x in RTL.
    val mirrored = horizontal && rtl
    val scrolled = if (horizontal) -bounds.left else -bounds.top
    val pos = if (mirrored) max - scrolled else scrolled
    val size = if (horizontal) viewport.width else viewport.height
    val target = scrollStep(pos.toDouble(), max.toDouble(), size.toDouble(), forward) ?: return false
    // Scrolling on moves the content the other way.
    val pan = (target - pos).toFloat() * if (mirrored) 1 else -1
    panBy(if (horizontal) Offset(pan, 0f) else Offset(0f, pan))
    return true
}

/** Sub-sampled by Telephoto: tiles are decoded from the disk cache for the visible region. */
@Composable
private fun SinglePage(
    pages: PageSource,
    index: Int,
    zoom: ZoomableState,
    scale: ContentScale,
    alignment: Alignment,
    onTap: (Offset, ZoomableState?) -> Unit,
) {
    val state = rememberZoomableImageState(zoom)
    val load = rememberPageLoad(pages, index)
    val label = stringResource(R.string.reader_page, index + 1)
    if (load.request != null) {
        ZoomableAsyncImage(
            model = load.request,
            // Named on the overlay, which covers the image's own semantics.
            contentDescription = null,
            // Pointer-blind: the overlay below is the one handler. The image's own detector can't be given a null
            // double click (Telephoto 0.19.0), and would hold every tap for a second one.
            modifier = Modifier.fillMaxSize(),
            state = state,
            alignment = alignment,
            contentScale = scale,
            gesturesEnabled = false,
        )
        // Over the image, which then gets no pointer events (siblings don't share them). Same zoom state, so the
        // image follows it. No double click: a tap is reported at once, and there is no one-finger quick zoom.
        Box(Modifier.fillMaxSize().zoomable(zoom, onClick = { onTap(it, zoom) }, onDoubleClick = null).semantics { contentDescription = label })
    }
    // The image takes no taps until it shows.
    if (load.request == null || !state.isImageDisplayed) PageStatus(load.error, load.retry, Modifier.onTaps { onTap(it, null) })
}

/**
 * Two pages in reading order, zoomed as one. Not sub-sampled: each is decoded at the size the fit
 * shows it, and the [settled] spread gets a larger decode on top while it is zoomed in.
 */
@Composable
private fun DoublePage(
    pages: PageSource,
    spread: List<Int>,
    zoom: ZoomableState,
    viewport: IntSize,
    fit: Fit,
    settled: Boolean,
    alignment: Alignment,
    rtl: Boolean,
    onTap: (Offset, ZoomableState?) -> Unit,
) {
    val scale = SpreadScale(fit, zoomWide = false)
    zoom.contentScale = scale
    zoom.contentAlignment = alignment
    val sizes = spread.map { pages.pages[it] }
    // The pair is laid out to fit the screen; the fit then scales it up like a single image.
    val layout = remember(sizes, viewport) {
        layoutSpread(sizes, Extent(viewport.width.toDouble(), viewport.height.toDouble()), Fit.Screen, zoomWide = false)
    }
    // Whole px, so rounding never makes a fitting spread overflow. Pages of unknown size share the
    // screen, which Telephoto shows as laid out.
    val widths = layout?.pages?.map { it.width.toInt() } ?: List(spread.size) { viewport.width / spread.size }
    val size = Size(widths.sum().toFloat(), layout?.height?.toInt()?.toFloat() ?: viewport.height.toFloat())
    val fitScale = if (layout == null) 1f else scale.computeScaleFactor(size, viewport.toSize()).scaleX
    var sharp by remember { mutableStateOf(false) }
    LaunchedEffect(zoom, size, fitScale) {
        snapshotFlow { zoom.bounds().width / (size.width * fitScale) }.collect { sharp = spreadSharp(it, sharp) }
    }
    val large = settled && sharp
    val label = stringResource(R.string.reader_pages, spread.first() + 1, spread.last() + 1)
    val order = if (rtl) spread.reversed() else spread
    Box(
        Modifier.fillMaxSize()
            // No double click: Telephoto then reports a tap at once, and arbitrates it with children and ancestors.
            .zoomable(zoom, onClick = { onTap(it, zoom) }, onDoubleClick = null).semantics { contentDescription = label },
        Alignment.Center,
    ) {
        if (layout != null) {
            LaunchedEffect(size) { zoom.setContentLocation(ZoomableContentLocation.scaledInsideAndCenterAligned(size)) }
        }
        with(LocalDensity.current) {
            Row(Modifier.requiredSize(size.width.toDp(), size.height.toDp())) {
                order.forEachIndexed { i, index ->
                    val width = widths[spread.indexOf(index)]
                    val slot = Extent(width.toDouble(), size.height.toDouble())
                    // Unknown sizes meet at the spine.
                    val toSpine = when {
                        layout != null -> Alignment.Center
                        i == 0 -> Alignment.CenterEnd
                        else -> Alignment.CenterStart
                    }
                    PageImage(pages, index, slot, fitScale, large, Modifier.width(width.toDp()).fillMaxHeight(), toSpine)
                }
            }
        }
    }
}

@Composable
private fun PageImage(
    pages: PageSource,
    index: Int,
    slot: Extent,
    fitScale: Float,
    large: Boolean,
    modifier: Modifier,
    alignment: Alignment,
) {
    val page = pages.pages[index]
    fun decode(scale: Float) = spreadDecodeSize(slot, page, scale).let { coil3.size.Size(it.width, it.height) }
    val base = rememberPageLoad(pages, index, decode(fitScale), exact = true)
    // The larger image's request, once it has loaded. The base isn't drawn under it, since a
    // transparent page would show both, but stays loaded for when it leaves or is requested anew.
    var loaded by remember { mutableStateOf<ImageRequest?>(null) }
    // No placeholder or spinner: the base shows until this has loaded, or if it fails.
    val overlay = if (large) rememberPageLoad(pages, index, decode(fitScale * MAX_ZOOM), exact = true).request else null
    // By identity: ImageRequest's equals would match a new, not yet loaded request for the same size.
    val covered = overlay != null && overlay === loaded
    // The spread is one node, "Pages N–M": its images aren't named again.
    Box(modifier) {
        PageLoadImage(
            base,
            contentDescription = null,
            Modifier.fillMaxSize().drawWithContent { if (!covered) drawContent() },
            alignment,
        )
        if (overlay != null) {
            AsyncImage(
                overlay,
                null,
                Modifier.fillMaxSize(),
                alignment = alignment,
                contentScale = ContentScale.Fit,
                onState = { loaded = overlay.takeIf { _ -> it is AsyncImagePainter.State.Success } },
            )
        }
    }
}
