package me.tijlvdb.voltis.ui.kit

import androidx.compose.animation.core.snap
import androidx.compose.animation.core.tween
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.border
import androidx.compose.foundation.background
import androidx.compose.foundation.focusable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.awaitVerticalTouchSlopOrCancellation
import androidx.compose.foundation.gestures.verticalDrag
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.wrapContentSize
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.systemGestureExclusion
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.input.pointer.PointerInputChange
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.LayoutCoordinates
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.layout.positionInRoot
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.ProgressBarRangeInfo
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.progressBarRangeInfo
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.setProgress
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlin.math.abs
import kotlin.math.roundToInt
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.domain.catalog.segmentAt
import me.tijlvdb.voltis.ui.animationsEnabled
import me.tijlvdb.voltis.ui.touchExploration

/** A place the rail jumps to: its [bubble] text, and where it [start]s (0-1 of the scrollable length). */
class RailSegment(val bubble: String, val start: Float)

/**
 * A handle at the end edge that drags through a long list (`AScrubber.vue`, handle mode). It fills its parent's
 * height as the track and takes touches only on the handle, which shows while [scrolling] and 1.5 s after, and
 * while dragged, focused, or under TalkBack. [value] is where the list is (0-1); [onSeek] gets where to go;
 * [onDrag] tells a drag from a rest. The hidden handle stays a focus target, so a keyboard can reveal it.
 */
@Composable
fun VScrubber(
    segments: List<RailSegment>,
    value: Float,
    onSeek: (Float) -> Unit,
    onDrag: (Boolean) -> Unit,
    scrolling: Boolean,
    modifier: Modifier = Modifier,
) {
    val label = stringResource(R.string.rail_jump_to)
    val scheme = MaterialTheme.colorScheme
    val density = LocalDensity.current
    val haptics = LocalHapticFeedback.current
    val scope = rememberCoroutineScope()
    val starts = remember(segments) { segments.map { it.start } }
    val seek by rememberUpdatedState(onSeek)
    val drag by rememberUpdatedState(onDrag)
    var dragging by remember { mutableStateOf(false) }
    var dragAt by remember { mutableFloatStateOf(0f) }
    var recent by remember { mutableStateOf(false) }
    var focused by remember { mutableStateOf(false) }
    var touched by remember { mutableStateOf(false) }
    var keyIndex by remember { mutableIntStateOf(-1) }
    var paused by remember { mutableStateOf(false) }
    var rest by remember { mutableStateOf<Job?>(null) }
    var track by remember { mutableStateOf<LayoutCoordinates?>(null) }
    var handle by remember { mutableStateOf<LayoutCoordinates?>(null) }
    LaunchedEffect(scrolling, dragging) {
        if (scrolling || dragging) {
            recent = true
        } else {
            delay(HIDE_MS)
            recent = false
        }
    }
    val shown = dragging || touched || recent || focused || touchExploration()
    val alpha by animateFloatAsState(if (shown) 1f else 0f, if (animationsEnabled()) tween(200) else snap(), label = "rail")
    val position = if (dragging) dragAt else value
    val dragged = segmentAt(starts, position)
    // The segment of a key press holds while the list sits at its start, so a rounding doesn't step again.
    val current = if (keyIndex in starts.indices && abs(position - starts[keyIndex]) < TOLERANCE) keyIndex else dragged
    LaunchedEffect(dragged) { if (dragging) haptics.performHapticFeedback(HapticFeedbackType.SegmentFrequentTick) }
    // A drag holds the loading; it goes on where the finger rests for a moment, and where the drag ends.
    fun settle(now: Boolean) {
        rest?.cancel()
        if (now) {
            paused = false
            drag(false)
        } else {
            rest = scope.launch {
                delay(REST_MS)
                paused = false
                drag(false)
            }
        }
    }
    fun move(to: Float) {
        dragAt = to.coerceIn(0f, 1f)
        seek(dragAt)
        if (!paused) {
            paused = true
            drag(true)
        }
        settle(false)
    }
    fun step(to: Int) {
        val i = to.coerceIn(0, starts.lastIndex)
        keyIndex = i
        seek(starts[i])
    }
    BoxWithConstraints(modifier.onGloballyPositioned { track = it }) {
        val travel = (constraints.maxHeight - with(density) { HANDLE_HEIGHT.toPx() }).coerceAtLeast(1f)
        val shape = RoundedCornerShape(topStart = 14.dp, bottomStart = 14.dp)
        Box(
            Modifier.align(Alignment.TopEnd).offset { IntOffset(0, (travel * position).roundToInt()) }.size(HANDLE_HEIGHT)
                .graphicsLayer { this.alpha = alpha },
        ) {
            if (dragging) {
                Text(
                    segments[dragged].bubble,
                    Modifier.align(Alignment.CenterEnd).clearAndSetSemantics {}.wrapContentSize(Alignment.CenterEnd, unbounded = true).padding(end = 28.dp + 8.dp)
                        .shadow(6.dp, MaterialTheme.shapes.extraSmall)
                        .background(scheme.inverseSurface, MaterialTheme.shapes.extraSmall)
                        .padding(horizontal = 10.dp, vertical = 6.dp),
                    color = scheme.inverseOnSurface,
                    fontSize = 13.sp,
                    fontWeight = FontWeight.SemiBold,
                )
            }
            Box(
                Modifier.size(HANDLE_HEIGHT)
                    .semantics(mergeDescendants = true) {
                        contentDescription = label
                        stateDescription = segments[current].bubble
                        progressBarRangeInfo = ProgressBarRangeInfo(current.toFloat(), 0f..starts.lastIndex.toFloat(), (starts.size - 2).coerceAtLeast(0))
                        // With two segments the slider is continuous to TalkBack, which asks for current ± a fraction.
                        setProgress {
                            val to = it.roundToInt()
                            step(if (to == current && it != current.toFloat()) current + if (it > current) 1 else -1 else to)
                            true
                        }
                    }
                    .onKeyEvent { event ->
                        if (event.type != KeyEventType.KeyDown) return@onKeyEvent false
                        when (event.key) {
                            Key.DirectionUp -> step(current - 1)
                            Key.DirectionDown -> step(current + 1)
                            Key.PageUp -> step(current - PAGE_STEP)
                            Key.PageDown -> step(current + PAGE_STEP)
                            Key.MoveHome -> step(0)
                            Key.MoveEnd -> step(starts.lastIndex)
                            else -> return@onKeyEvent false
                        }
                        true
                    }
                    .onFocusChanged { focused = it.isFocused }
                    .focusable()
                    .onGloballyPositioned { handle = it }
                    // A hidden handle takes no touches, which then reach the grid, and no gesture exclusion.
                    .then(
                        if (alpha <= 0f) Modifier else Modifier.systemGestureExclusion().pointerInput(travel) {
                            awaitEachGesture {
                                val down = awaitFirstDown(requireUnconsumed = false)
                                // Shown for the whole touch, so a fading handle isn't taken away mid-gesture.
                                touched = true
                                try {
                                    // The finger's offset in the handle, which then follows it. The handle moves under the finger,
                                    // so its position is read in the window's coordinates, not the handle's own.
                                    val grab = down.position.y
                                    val slop = awaitVerticalTouchSlopOrCancellation(down.id) { change, _ -> change.consume() } ?: return@awaitEachGesture
                                    fun to(change: PointerInputChange) =
                                        (handle!!.localToRoot(change.position).y - grab - track!!.positionInRoot().y) / travel
                                    dragging = true
                                    paused = true
                                    drag(true)
                                    move(to(slop))
                                    try {
                                        verticalDrag(slop.id) { change ->
                                            change.consume()
                                            move(to(change))
                                        }
                                    } finally {
                                        dragging = false
                                        settle(true)
                                    }
                                } finally {
                                    touched = false
                                }
                            }
                        },
                    ),
                Alignment.CenterEnd,
            ) {
                Column(
                    Modifier.size(28.dp, HANDLE_HEIGHT).shadow(6.dp, shape).background(scheme.surfaceContainerHigh, shape)
                        .then(if (focused) Modifier.border(2.dp, scheme.primary, shape) else Modifier),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.Center,
                ) {
                    Icon(VIcons.ChevronUp, null, Modifier.size(16.dp), scheme.onSurfaceVariant)
                    Icon(VIcons.ChevronDown, null, Modifier.size(16.dp), scheme.onSurfaceVariant)
                }
            }
        }
    }
}

private val HANDLE_HEIGHT = 48.dp
private const val HIDE_MS = 1_500L
private const val REST_MS = 150L
private const val PAGE_STEP = 5
private const val TOLERANCE = 1e-3f
