package me.tijlvdb.voltis.ui.nav

import android.content.Intent
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** Where an intent from outside the app asks to go: a launcher shortcut, or the download notification (P5 §6). */
sealed interface LaunchTarget {
    data object Search : LaunchTarget
    data object Downloads : LaunchTarget
    data object Continue : LaunchTarget

    companion object {
        const val ACTION_SEARCH = "me.tijlvdb.voltis.action.SEARCH"
        const val ACTION_DOWNLOADS = "me.tijlvdb.voltis.action.DOWNLOADS"
        const val ACTION_CONTINUE = "me.tijlvdb.voltis.action.CONTINUE"
    }
}

/** Null for any other action, and for an intent that Recents replayed (FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY). */
fun launchTargetOf(action: String?, flags: Int): LaunchTarget? {
    if (flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY != 0) return null
    return when (action) {
        LaunchTarget.ACTION_SEARCH -> LaunchTarget.Search
        LaunchTarget.ACTION_DOWNLOADS -> LaunchTarget.Downloads
        LaunchTarget.ACTION_CONTINUE -> LaunchTarget.Continue
        else -> null
    }
}

/**
 * The target offered last and not yet taken. It outlives a recreated activity; `AppNav` takes it once
 * the session has settled, and a target is taken only after what it opens is known.
 */
@Singleton
class LaunchTargets @Inject constructor() {
    private val state = MutableStateFlow<LaunchTarget?>(null)
    val pending: StateFlow<LaunchTarget?> = state.asStateFlow()

    fun offer(target: LaunchTarget) {
        state.value = target
    }

    /** A fresh activity's own target, or none: one left from an activity that finished before taking it is dropped. */
    fun set(target: LaunchTarget?) {
        state.value = target
    }

    /** False when [expected] was replaced by a newer target meanwhile. */
    fun take(expected: LaunchTarget): Boolean = state.compareAndSet(expected, null)
}
