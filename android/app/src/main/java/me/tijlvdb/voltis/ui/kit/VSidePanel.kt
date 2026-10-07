package me.tijlvdb.voltis.ui.kit

import androidx.activity.compose.PredictiveBackHandler
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.foundation.background
import androidx.compose.foundation.focusGroup
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.isTraversalGroup
import androidx.compose.ui.semantics.paneTitle
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.traversalIndex
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import me.tijlvdb.voltis.ui.rememberTouchExploration
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/**
 * A titled panel at the window's end, as the web's reader drawer: it sits in a [Row] beside the
 * page, which gives up its width, so there is no scrim and what the panel changes stays in view.
 * A tap on the page is for the caller to turn into [onDismiss]. Back and the close button dismiss
 * it. Focus moves to the panel when it opens, and to [anchor]'s button for TalkBack when it
 * closes. It slides in and out, following the system's animator scale.
 */
@Composable
fun VSidePanel(
    visible: Boolean,
    title: String,
    onDismiss: () -> Unit,
    width: Dp = 360.dp,
    anchor: PopoverAnchor? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    // The page beside the panel changes its width once, as the panel starts to open or when it
    // has gone: only the panel moves in between, so the reader isn't laid out on every frame.
    var drag by remember { mutableFloatStateOf(0f) }
    val focus = remember { FocusRequester() }
    // The panel sits in the page's window, so TalkBack's cursor stays where it was: move it to the heading.
    val view = LocalView.current
    val explore by rememberTouchExploration()
    val titleNode = remember { PopoverAnchor() }
    LaunchedEffect(visible, explore) {
        if (visible && explore) {
            delay(PanelFocusDelayMs)
            focusForTalkBack(view, titleNode)
        }
    }
    // Each time it opens, including during an exit: the gesture's progress is stale and focus has left.
    LaunchedEffect(visible) {
        if (visible) {
            drag = 0f
            runCatching { focus.requestFocus() }
        }
    }
    // The panel is at the end: mirrored when the layout is.
    val sign = if (LocalLayoutDirection.current == LayoutDirection.Rtl) -1 else 1
    AnimatedVisibility(visible, enter = slideInHorizontally { it * sign } + fadeIn(), exit = slideOutHorizontally { it * sign } + fadeOut()) {
        PredictiveBackHandler(visible) { progress ->
            try {
                progress.collect { drag = it.progress }
                onDismiss()
            } catch (e: CancellationException) {
                drag = 0f
                throw e
            }
        }
        AnchorFocusReturn(anchor)
        val colors = VoltisTheme.colors
        Row(Modifier.fillMaxHeight().graphicsLayer { translationX = drag * size.width * 0.15f * sign; alpha = 1f - drag * 0.3f }) {
            Box(Modifier.width(1.dp).fillMaxHeight().background(colors.cardLine))
            Column(
                Modifier
                    .width(width)
                    .fillMaxHeight()
                    .background(MaterialTheme.colorScheme.surfaceContainerLow)
                    .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.End + WindowInsetsSides.Vertical))
                    .semantics { paneTitle = title; isTraversalGroup = true; traversalIndex = 2f }
                    .focusRequester(focus)
                    .focusGroup(),
            ) {
                Row(
                    Modifier.fillMaxWidth().heightIn(min = 56.dp).padding(start = 24.dp, end = 8.dp),
                    Arrangement.SpaceBetween,
                    Alignment.CenterVertically,
                ) {
                    Text(title, Modifier.popoverAnchor(titleNode).semantics { heading() }, style = MaterialTheme.typography.titleLarge)
                    VIconButton(VIcons.Close, stringResource(R.string.close), onDismiss)
                }
                val scroll = rememberScrollState()
                if (scroll.canScrollBackward) HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                Column(Modifier.weight(1f).verticalScroll(scroll).padding(horizontal = 24.dp).padding(bottom = 24.dp), content = content)
            }
        }
    }
}

/** After the slide-in has put the heading in the tree. */
private const val PanelFocusDelayMs = 400L
