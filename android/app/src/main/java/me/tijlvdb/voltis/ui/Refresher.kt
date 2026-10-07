package me.tijlvdb.voltis.ui

import android.os.SystemClock
import androidx.compose.runtime.Composable
import androidx.lifecycle.compose.LifecycleResumeEffect
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.content.CatalogSignal

/**
 * Keeps a screen's data current. A catalog change that [matches] marks the screen dirty, and a
 * dirty screen refreshes when it next resumes, or at once if it is resumed. It also refreshes
 * on a resume after more than 30 seconds away. Resumed, it refreshes at once, and changes within
 * the next 500 ms add one more refresh at the end of that window: a burst of writes costs two.
 * A bulk batch costs one: its changes, stamped with its id, are held until its end (or its touched items) says to refresh.
 * Every screen's [matches] accepts [CatalogChange.OnlineChanged], so it reloads once when the server comes or goes.
 *
 * Events are collected in [scope], the view model's: a screen under the reader or in another tab
 * isn't resumed, and the flow has no replay.
 */
class Refresher(
    private val scope: CoroutineScope,
    events: CatalogEvents,
    matches: (CatalogChange) -> Boolean,
    private val now: () -> Long = SystemClock::elapsedRealtime,
    private val refresh: () -> Unit,
) {
    private var dirty = false
    private var resumed = false
    private var pausedAt: Long? = null
    private var window: Job? = null
    private var pending = false
    private val held = mutableSetOf<Long>() // batches with a matching change seen here

    init {
        scope.launch {
            events.signals.collect { signal ->
                when (signal) {
                    is CatalogSignal.Changed -> when {
                        !matches(signal.change) -> {}
                        signal.batch != null -> held += signal.batch
                        else -> changed()
                    }
                    // `or`: always forget the id.
                    is CatalogSignal.BatchEnded -> if (held.remove(signal.batch) or signal.touched.any(matches)) changed()
                }
            }
        }
    }

    /** As a matching change: the screen's own writes, done elsewhere. */
    fun changed() {
        when {
            !resumed -> dirty = true
            window?.isActive == true -> pending = true
            else -> fire()
        }
    }

    private fun fire() {
        pending = false
        refresh()
        window = scope.launch {
            delay(THROTTLE_MS)
            if (pending) fire()
        }
    }

    fun onResume() {
        resumed = true
        val stale = pausedAt?.let { now() - it > STALE_MS } ?: false
        if (dirty || stale) {
            window?.cancel()
            fire()
        }
        dirty = false
    }

    fun onPause() {
        resumed = false
        pausedAt = now()
        window?.cancel()
        if (pending) dirty = true
        pending = false
    }

    private companion object {
        const val STALE_MS = 30_000L
        const val THROTTLE_MS = 500L
    }
}

/** Ties [refresher] to the screen: resumed while it is shown and the app is in front. */
@Composable
fun RefreshOnResume(refresher: Refresher) {
    LifecycleResumeEffect(refresher) {
        refresher.onResume()
        onPauseOrDispose { refresher.onPause() }
    }
}
