package me.tijlvdb.voltis.ui

import android.view.accessibility.AccessibilityManager
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.State
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.MotionDurationScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp

/** Whether TalkBack explores the screen by touch, so taps no longer reach the app as taps. */
@Composable
fun touchExploration(): Boolean = rememberTouchExploration().value

/** [touchExploration] as state, for code that reads it later than composition. */
@Composable
fun rememberTouchExploration(): State<Boolean> {
    val manager = LocalContext.current.getSystemService(AccessibilityManager::class.java)
    val enabled = remember(manager) { mutableStateOf(manager.isTouchExplorationEnabled) }
    DisposableEffect(manager) {
        val listener = AccessibilityManager.TouchExplorationStateChangeListener { enabled.value = it }
        manager.addTouchExplorationStateChangeListener(listener)
        onDispose { manager.removeTouchExplorationStateChangeListener(listener) }
    }
    return enabled
}

/**
 * Says texts to TalkBack through two polite live regions, placed by [Regions]. Each [say] fills the
 * region it emptied last time and empties the other, so the same words twice are two changes, and
 * TalkBack speaks both (P4 task phase 6, the spike). A text is said at a moment of the screen (its
 * attention count) and is gone once that moves on, so an old one isn't left for TalkBack to find.
 */
class Announcer {
    private var first by mutableStateOf<String?>(null)
    private var second by mutableStateOf<String?>(null)
    private var saidAt by mutableStateOf<Any?>(null)
    private var toFirst = true

    /** [at]: the screen's moment now. */
    fun say(text: String, at: Any) {
        if (toFirst) {
            first = text
            second = null
        } else {
            second = text
            first = null
        }
        saidAt = at
        toFirst = !toFirst
    }

    /** Outside any lazy list, so they are there however far it scrolled. Empty once [now] isn't the moment of the last [say]. */
    @Composable
    fun Regions(now: Any) {
        val current = saidAt == now
        Region(first.takeIf { current })
        Region(second.takeIf { current })
    }

    @Composable
    private fun Region(text: String?) {
        Box(
            Modifier.size(1.dp).semantics {
                liveRegion = LiveRegionMode.Polite
                text?.let { contentDescription = it }
            },
        )
    }
}

@Composable
fun rememberAnnouncer() = remember { Announcer() }

/**
 * False when the system's animator scale is 0 ("Remove animations"). Compose's own animations
 * follow it by themselves; others ask here. The scale Compose tracks is snapshot state, so this
 * follows a change while the app is alive.
 */
@Composable
fun animationsEnabled(): Boolean {
    return rememberCoroutineScope().coroutineContext[MotionDurationScale]?.scaleFactor != 0f
}
