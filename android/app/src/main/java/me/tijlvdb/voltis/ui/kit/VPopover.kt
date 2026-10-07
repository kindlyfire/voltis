package me.tijlvdb.voltis.ui.kit

import android.view.View
import android.view.accessibility.AccessibilityNodeInfo.ACTION_ACCESSIBILITY_FOCUS
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.focusGroup
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.node.RootForTest
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.semantics.SemanticsPropertyKey
import androidx.compose.ui.semantics.SemanticsPropertyReceiver
import androidx.compose.ui.semantics.getAllSemanticsNodes
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.paneTitle
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntRect
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Popup
import androidx.compose.ui.window.PopupPositionProvider
import androidx.compose.ui.window.PopupProperties
import me.tijlvdb.voltis.ui.isLarge
import me.tijlvdb.voltis.ui.rememberTouchExploration
import me.tijlvdb.voltis.ui.theme.VoltisShapes
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/**
 * A titled panel of controls that stays open while they are used, as the web's `APopover`: on a
 * large window (`isLarge()`) a popover of [width] anchored to the composable it is placed in, else
 * a [VSheet], which has more room on a phone on its side. It opens below its anchor, or above
 * when there is more room there, and scrolls when taller than that room. Back or a tap outside
 * dismisses it. [action] sits at the heading's end. Under TalkBack, focus goes back to [anchor]
 * once it closes; the system would leave it at the window's start.
 */
@Composable
fun VPopover(
    title: String,
    onDismiss: () -> Unit,
    width: Dp = 320.dp,
    action: (@Composable () -> Unit)? = null,
    anchor: PopoverAnchor? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    AnchorFocusReturn(anchor)
    if (!isLarge()) return VSheet(title, onDismiss, action = action, content = content)
    val density = LocalDensity.current
    val direction = LocalLayoutDirection.current
    // The popup's own window size leaves out the system bars while its anchor is measured in the
    // edge-to-edge window: the room is measured against that window less the safe-drawing insets.
    val window = LocalWindowInfo.current.containerSize
    val insets = WindowInsets.safeDrawing
    val safe = IntRect(
        insets.getLeft(density, direction),
        insets.getTop(density),
        window.width - insets.getRight(density, direction),
        window.height - insets.getBottom(density),
    )
    // The room on the chosen side, in px, known once the anchor has been placed.
    var room by remember { mutableStateOf<Int?>(null) }
    val placement = remember(density, safe) {
        val (gap, margin, minBelow) = with(density) { listOf(Gap.roundToPx(), Margin.roundToPx(), MinBelow.roundToPx()) }
        object : PopupPositionProvider {
            override fun calculatePosition(anchorBounds: IntRect, windowSize: IntSize, layoutDirection: LayoutDirection, popupContentSize: IntSize): IntOffset {
                val spot = placePopover(anchorBounds, safe, popupContentSize, layoutDirection == LayoutDirection.Rtl, gap, margin, minBelow)
                if (room != spot.room) room = spot.room
                return spot.offset
            }
        }
    }
    val focus = remember { FocusRequester() }
    // Focus moves into the panel, to its first control, as the web's popover does.
    LaunchedEffect(room != null) { if (room != null) focus.requestFocus() }
    Popup(placement, onDismiss, PopupProperties(focusable = true)) {
        val colors = VoltisTheme.colors
        Column(
            Modifier
                .width(width)
                .heightIn(max = room?.let { with(density) { it.toDp() } } ?: Dp.Unspecified)
                // Hidden for the one frame measured before the room is known.
                .graphicsLayer { alpha = if (room == null) 0f else 1f }
                .shadow(6.dp, VoltisShapes.menu)
                .background(colors.raised, VoltisShapes.menu)
                .border(1.dp, colors.cardLine, VoltisShapes.menu)
                .semantics { paneTitle = title }
                .focusRequester(focus)
                .focusGroup(),
        ) {
            Row(
                Modifier.fillMaxWidth().heightIn(min = 48.dp).padding(start = 16.dp, end = 16.dp, top = 8.dp),
                Arrangement.SpaceBetween,
                Alignment.CenterVertically,
            ) {
                Text(title, Modifier.semantics { heading() }, style = MaterialTheme.typography.titleLarge)
                action?.invoke()
            }
            val scroll = rememberScrollState()
            // Content scrolled under the heading: the line says there is more above.
            if (scroll.canScrollBackward) HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            Column(
                Modifier.weight(1f, fill = false).verticalScroll(scroll).padding(start = 16.dp, end = 16.dp, bottom = 16.dp),
                content = content,
            )
        }
    }
}

/**
 * Under TalkBack, puts accessibility focus back on [anchor]'s button once this leaves the
 * composition; the system would leave it at the window's start.
 */
@Composable
fun AnchorFocusReturn(anchor: PopoverAnchor?) {
    val view = LocalView.current
    val explore by rememberTouchExploration()
    DisposableEffect(anchor) {
        onDispose {
            if (anchor == null || !explore) return@onDispose
            // After TalkBack has moved to the window's start, which it does a little after the panel's window goes.
            // The node is looked up then, not now: the exposed control may change meanwhile (VDisabled).
            view.postDelayed({ focusForTalkBack(view, anchor) }, FocusReturnDelayMs)
        }
    }
}

/** The button a [VPopover] opens from: put [popoverAnchor] on it and pass it to the popover. */
class PopoverAnchor

/** Marks this as [anchor]'s button. */
fun Modifier.popoverAnchor(anchor: PopoverAnchor): Modifier = semantics { this[AnchorKey] = anchor }

/** [popoverAnchor] for a semantics block, such as a [VDisabled]'s. */
fun SemanticsPropertyReceiver.popoverAnchor(anchor: PopoverAnchor) {
    this[AnchorKey] = anchor
}

private val AnchorKey = SemanticsPropertyKey<PopoverAnchor>("PopoverAnchor")

/** The accessibility node id of [anchor]'s button in [view], from its semantics tree. */
private fun anchorId(view: View, anchor: PopoverAnchor): Int? =
    (view as? RootForTest)?.semanticsOwner?.getAllSemanticsNodes(mergingEnabled = false)?.firstOrNull { it.config.getOrNull(AnchorKey) === anchor }?.id

/** Moves TalkBack's cursor to [anchor]'s node, if it is in [view]'s tree. */
fun focusForTalkBack(view: View, anchor: PopoverAnchor) {
    val id = anchorId(view, anchor) ?: return
    view.accessibilityNodeProvider?.performAction(id, ACTION_ACCESSIBILITY_FOCUS, null)
}

/** 300 ms was too early for TalkBack in three of four tries; 800 ms worked every time. */
private const val FocusReturnDelayMs = 800L

private val Gap = 4.dp
private val Margin = 8.dp

/** Below the anchor whenever this much room is left there. */
private val MinBelow = 360.dp

/** Where a popover of [size] goes ([offset]), and the height it may take there ([room]). */
internal data class PopoverSpot(val offset: IntOffset, val room: Int)

/**
 * A popover at its [anchor]'s start edge, below it when [minBelow] is left there or there is more
 * room below than above, else above; kept [margin] inside [safe], the window less its insets. All
 * in px of the window.
 */
internal fun placePopover(anchor: IntRect, safe: IntRect, size: IntSize, rtl: Boolean, gap: Int, margin: Int, minBelow: Int): PopoverSpot {
    val below = safe.bottom - anchor.bottom - gap - margin
    val above = anchor.top - safe.top - gap - margin
    val down = below >= minBelow || below >= above
    val x = if (rtl) anchor.right - size.width else anchor.left
    val y = if (down) anchor.bottom + gap else anchor.top - gap - size.height
    val left = safe.left + margin
    return PopoverSpot(
        IntOffset(x.coerceIn(left, maxOf(left, safe.right - margin - size.width)), y.coerceAtLeast(safe.top + margin)),
        (if (down) below else above).coerceAtLeast(0),
    )
}
