package me.tijlvdb.voltis.ui

import android.util.Log
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.snapshotFlow
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import java.util.concurrent.atomic.AtomicLong
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first

data class Posted<E>(val id: Long, val effect: E)

/** UI work a view model asks of its screen, kept until done. Main thread only. */
class Effects<E : Any> {
    private val queue = mutableStateListOf<Posted<E>>()

    val head: Posted<E>? get() = queue.firstOrNull()

    fun post(effect: E) {
        queue += Posted(ids.incrementAndGet(), effect)
    }

    fun done(p: Posted<E>) {
        queue.remove(p)
    }

    private companion object {
        /** Process-wide, so two equal effects are two entries. */
        val ids = AtomicLong()
    }
}

/**
 * Handles each head until [handle] returns, then acknowledges it. A cancelled [handle] leaves the head
 * queued and runs again from the start, so before its final commit [handle] may only wait or do work
 * that may be repeated (a scroll). The commit (message, announcement, navigation) is non-suspending and last.
 * A [handle] that throws drops its effect, reported to [failed], so one bad effect doesn't end the host or the app.
 */
suspend fun <E : Any> Effects<E>.drain(failed: (Exception) -> Unit = {}, handle: suspend (E) -> Unit) {
    while (true) {
        val h = snapshotFlow { head }.filterNotNull().first()
        try {
            handle(h.effect)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            failed(e)
        }
        done(h)
    }
}

/** Drains [effects] while the screen is started. */
@Composable
fun <E : Any> EffectHost(effects: Effects<E>, handle: suspend (E) -> Unit) {
    val current by rememberUpdatedState(handle)
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(effects, lifecycle) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { effects.drain({ Log.w("EffectHost", "An effect failed", it) }) { current(it) } }
    }
}
