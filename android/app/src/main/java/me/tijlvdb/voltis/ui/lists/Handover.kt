package me.tijlvdb.voltis.ui.lists

import androidx.compose.runtime.snapshotFlow
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch

/** What a handover aims at, from the displayed rows: not yet, nothing, or a row (null: the title). */
sealed interface Target {
    data object Wait : Target

    data object Skip : Target

    data class Go(val id: String?) : Target
}

/** What [handOver] reads and does on a list's page. Everything it reads is snapshot state. */
interface HandoverPorts {
    /** The displayed rows' entry IDs. */
    val displayed: List<String>

    /** The lazy list's index of [target]'s item among [displayed]; the header (the title) is 0. */
    fun lazyIndex(target: String?): Int

    /** The layout still has [target]'s item at another index than [lazyIndex]: it hasn't caught up with [displayed]. */
    fun lagging(target: String?): Boolean

    /** [target]'s own item, found by its key, is laid out whole inside the viewport. */
    fun fullyVisible(target: String?): Boolean

    fun partlyVisible(target: String?): Boolean

    /** [target]'s focus anchor is placed. */
    fun anchored(target: String?): Boolean

    fun requestFocus(target: String?)

    suspend fun scrollTo(index: Int)
}

/**
 * Waits until [target]'s row is placed and at least partly visible, then requests focus for it once
 * and answers true. The target is recomputed from the live displayed rows at each change. A row not
 * fully visible is scrolled to once. False, without focus, when [valid] lapses (at any point: a
 * pending scroll is cancelled) or the target is [Target.Skip].
 */
suspend fun handOver(intent: FocusIntent?, valid: (FocusIntent?) -> Boolean, ports: HandoverPorts, target: (List<String>) -> Target): Boolean {
    if (!valid(intent)) return false
    val ready = coroutineScope {
        val work = async { readyWhileValid(intent, valid, ports, target) }
        val watch = launch {
            snapshotFlow { valid(intent) }.first { !it }
            work.cancel()
        }
        try {
            work.await()
        } catch (e: CancellationException) {
            // Our own cancellation goes on; the watch's is a handover that settles without focus.
            currentCoroutineContext().ensureActive()
            null
        } finally {
            watch.cancel()
        }
    } ?: return false
    // Nothing suspends from here on: a handover cancelled above runs again without having asked for focus.
    if (!valid(intent) || (ready.id != null && ready.id !in ports.displayed)) return false
    ports.requestFocus(ready.id)
    return true
}

private sealed interface Step {
    data object Stop : Step

    data class Scroll(val target: String?, val index: Int) : Step

    data class Focus(val target: String?) : Step
}

/** The target once it can take focus; null when the handover settles without it. */
private suspend fun readyWhileValid(intent: FocusIntent?, valid: (FocusIntent?) -> Boolean, ports: HandoverPorts, target: (List<String>) -> Target): Target.Go? {
    val scrolled = HashSet<String?>()
    while (true) {
        // Read in full each time, so a change in any of them (also only placement) is seen.
        val step = snapshotFlow {
            if (!valid(intent)) return@snapshotFlow Step.Stop
            when (val aim = target(ports.displayed)) {
                Target.Wait -> null
                Target.Skip -> Step.Stop
                is Target.Go -> {
                    val id = aim.id
                    when {
                        id != null && id !in ports.displayed -> null
                        // The layout is of an earlier list: its places say nothing yet.
                        ports.lagging(id) -> null
                        !ports.fullyVisible(id) && id !in scrolled -> Step.Scroll(id, ports.lazyIndex(id))
                        // A row taller than the screen is ready once part of it shows.
                        ports.partlyVisible(id) && ports.anchored(id) -> Step.Focus(id)
                        else -> null
                    }
                }
            }
        }.filterNotNull().first()
        when (step) {
            Step.Stop -> return null
            is Step.Scroll -> {
                if (!valid(intent)) return null
                scrolled += step.target
                ports.scrollTo(step.index)
            }
            is Step.Focus -> if (valid(intent) && (step.target == null || step.target in ports.displayed)) return Target.Go(step.target)
        }
    }
}

/**
 * A moved row's target. The move's reload is cache revision [revision]: while an older one is
 * [displayed] it hasn't arrived, and only then is a wait right. From it on the rows are the reload's
 * or a newer refresh's (also one that restored the same order), and the handover goes by the live
 * row, never waiting for a place only the reload knew. Skipped when the list isn't [present].
 */
fun movedTarget(entryId: String, revision: Long, displayed: Long, present: Boolean, shown: List<String>): Target = when {
    !present || entryId !in shown -> Target.Skip
    displayed < revision -> Target.Wait
    else -> Target.Go(entryId)
}

/**
 * A removal's target, by the same rule: wait only while the reload's [revision] isn't [displayed].
 * A list that isn't [present] (deleted meanwhile) has no revision to wait for and no row to focus: skip.
 */
fun removedTarget(gone: Set<String>, order: List<String>, revision: Long, displayed: Long, present: Boolean, shown: List<String>): Target = when {
    !present -> Target.Skip
    displayed < revision -> Target.Wait
    else -> Target.Go(removalTarget(order, gone, shown))
}

/**
 * Where focus goes after a removal: the first row after the removed one in [order] that is still
 * shown, else the nearest one before it, else null (the title). [gone] holds the removed row's IDs.
 */
fun removalTarget(order: List<String>, gone: Set<String>, shown: List<String>): String? {
    val at = order.indexOfFirst { it in gone }
    val survives = { id: String -> id in shown && id !in gone }
    if (at < 0) return null
    return order.drop(at + 1).firstOrNull(survives) ?: order.take(at).lastOrNull(survives)
}
