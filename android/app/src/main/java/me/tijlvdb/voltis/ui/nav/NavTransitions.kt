package me.tijlvdb.voltis.ui.nav

import androidx.compose.animation.AnimatedContentTransitionScope
import androidx.compose.animation.ContentTransform
import androidx.compose.animation.EnterTransition
import androidx.compose.animation.ExitTransition
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.ui.animationsEnabled
import androidx.navigation3.scene.Scene

/** Metadata key: the [Tab] an entry belongs to. Absent on the reader, which sits above the tabs. */
const val TAB_KEY = "tab"

enum class NavMotion {
    /** Material shared axis X: within one tab. */
    Slide,

    /** A fade-through, for the full-screen reader. */
    Fade,

    /** Switching tabs: the nav bar's indicator is the only motion. */
    None,
}

/** What animates between two top entries; a null tab is the reader. */
fun navMotion(from: Tab?, to: Tab?): NavMotion = when {
    from == null || to == null -> NavMotion.Fade
    from != to -> NavMotion.None
    else -> NavMotion.Slide
}

/** The sign of the incoming screen's offset: it enters from the end edge on a push, from the start on a pop. */
fun slideSign(pop: Boolean, rtl: Boolean): Int = (if (pop) -1 else 1) * (if (rtl) -1 else 1)

internal val Emphasized = CubicBezierEasing(0.2f, 0f, 0f, 1f)
private const val SLIDE_MS = 300
internal const val HERO_MS = SLIDE_MS
private const val FADE_OUT_MS = 90
private const val FADE_IN_MS = 210
private const val SLIDE_DP = 30

private fun Scene<Entry>.tab() = entries.last().metadata[TAB_KEY] as? Tab

/** Transition specs for NavDisplay. Durations follow the system animator scale (Compose's motion scale). */
class NavTransitions(private val slidePx: Int, private val rtl: Boolean, private val animations: Boolean) {
    private fun AnimatedContentTransitionScope<Scene<Entry>>.spec(pop: Boolean): ContentTransform {
        val direction = slideSign(pop, rtl)
        return when (navMotion(initialState.tab(), targetState.tab())) {
            NavMotion.None -> ContentTransform(EnterTransition.None, ExitTransition.None)
            NavMotion.Fade -> fadeIn(tween(FADE_IN_MS, FADE_OUT_MS, Emphasized)) togetherWith fadeOut(tween(FADE_OUT_MS))
            NavMotion.Slide -> (
                slideInHorizontally(tween(SLIDE_MS, easing = Emphasized)) { direction * slidePx } +
                    fadeIn(tween(FADE_IN_MS, FADE_OUT_MS, Emphasized))
                ) togetherWith (
                slideOutHorizontally(tween(SLIDE_MS, easing = Emphasized)) { -direction * slidePx } +
                    fadeOut(tween(FADE_OUT_MS, easing = Emphasized))
                )
        }.also {
            // The closing screen draws over the one it reveals.
            if (pop) it.targetContentZIndex = -1f
        }
    }

    val push: AnimatedContentTransitionScope<Scene<Entry>>.() -> ContentTransform = { spec(pop = false) }
    val pop: AnimatedContentTransitionScope<Scene<Entry>>.() -> ContentTransform = { spec(pop = true) }
    // Seeking ignores the animator duration scale, so with animations off the gesture moves nothing.
    val predictivePop: AnimatedContentTransitionScope<Scene<Entry>>.(Int) -> ContentTransform = {
        if (animations) spec(pop = true) else ContentTransform(EnterTransition.None, ExitTransition.None)
    }
}

@Composable
fun rememberNavTransitions(): NavTransitions {
    val density = LocalDensity.current
    val rtl = LocalLayoutDirection.current == LayoutDirection.Rtl
    val animations = animationsEnabled()
    return remember(density, rtl, animations) { NavTransitions(with(density) { SLIDE_DP.dp.roundToPx() }, rtl, animations) }
}
