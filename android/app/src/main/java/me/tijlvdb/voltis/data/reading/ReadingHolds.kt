package me.tijlvdb.voltis.data.reading

import java.util.concurrent.ConcurrentHashMap
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow

/**
 * Whether an account's reading engine holds a mutation that isn't stored (P2 §17): an event's batch
 * before its commit, or a refused batch until a commit succeeds. Automatic deletes wait for it,
 * because a delete that reads only Room could remove a volume whose Clear is still in memory.
 * Process-wide, keyed by the account directory's name, like `Pins`. Counts nothing and does no I/O.
 */
internal object ReadingHolds {
    private class Hold {
        @Volatile var held = false
        val released = MutableStateFlow(0L)
    }

    private val holds = ConcurrentHashMap<String, Hold>()

    private fun of(dir: String) = holds.getOrPut(dir) { Hold() }

    /** A clear that changes the value increments [released], after the flag is false. */
    fun set(dir: String, held: Boolean) {
        val hold = of(dir)
        synchronized(hold) {
            if (hold.held == held) return
            hold.held = held
            if (!held) hold.released.value += 1
        }
    }

    fun held(dir: String): Boolean = of(dir).held

    /** Incremented each time the hold of [dir] is cleared. */
    fun released(dir: String): StateFlow<Long> = of(dir).released
}
