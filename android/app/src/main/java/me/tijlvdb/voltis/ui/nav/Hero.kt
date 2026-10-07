package me.tijlvdb.voltis.ui.nav

import androidx.compose.animation.ExperimentalSharedTransitionApi
import androidx.compose.animation.SharedTransitionScope
import androidx.compose.animation.core.tween
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Stable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.navigation3.ui.LocalNavAnimatedContentScope
import me.tijlvdb.voltis.ui.animationsEnabled

/**
 * Identifies a cover that grows into a content page and shrinks back into its card (the hero
 * transition). [origin] is the screen (and row) the card is on, so the same item twice on one
 * screen, or on two, never matches the wrong card.
 */
data class HeroKey(val contentId: String, val origin: String)

/** The shared-transition layout around the NavDisplay; null outside it (the kit gallery, previews). */
@OptIn(ExperimentalSharedTransitionApi::class)
val LocalSharedTransitionScope = staticCompositionLocalOf<SharedTransitionScope?> { null }

/** The origin the cards of the current screen are tagged with. */
val LocalHeroOrigin = staticCompositionLocalOf { "" }

/** The origin a content page was opened from; null when it wasn't opened from a card. */
val LocalHeroTarget = staticCompositionLocalOf<String?> { null }

val LocalHeroTaps = staticCompositionLocalOf { HeroTaps() }

/**
 * The origin of the card tapped last. Cards open content through a plain `(String) -> Unit`, so the
 * tap leaves its origin here for the navigation to pick up.
 */
@Stable
class HeroTaps {
    private var key: HeroKey? = null
    private var at = 0L

    fun tap(key: HeroKey) {
        this.key = key
        at = System.nanoTime()
    }

    /** The origin of the tap that opened [contentId], once, and only if it just happened (a tap that opened nothing goes stale). */
    fun take(contentId: String): String? {
        val tapped = key
        key = null
        return tapped?.takeIf { it.contentId == contentId && System.nanoTime() - at < 1_000_000_000L }?.origin
    }
}

/** The hero's own motion: the page's shared axis, so the cover keeps pace with it. */
private val HeroBounds = tween<androidx.compose.ui.geometry.Rect>(HERO_MS, easing = Emphasized)

/**
 * Makes the cover a shared element with its twin on the page it opens or returns to. Nothing
 * happens without a twin, or with animations off. Call it in the place the cover is drawn; what follows it
 * in the chain is what flies.
 */
@OptIn(ExperimentalSharedTransitionApi::class)
@Composable
fun Modifier.heroElement(key: HeroKey?): Modifier {
    val scope = LocalSharedTransitionScope.current
    if (key == null || scope == null || !animationsEnabled()) return this
    val visible = LocalNavAnimatedContentScope.current
    return with(scope) {
        this@heroElement.sharedElement(
            rememberSharedContentState(key),
            visible,
            boundsTransform = { _, _ -> HeroBounds },
        )
    }
}
