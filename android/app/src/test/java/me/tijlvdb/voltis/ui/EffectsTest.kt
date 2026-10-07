package me.tijlvdb.voltis.ui

import androidx.compose.runtime.snapshots.Snapshot
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.yield
import org.junit.Assert.assertEquals
import org.junit.Test

class EffectsTest {
    private suspend fun settle() {
        Snapshot.sendApplyNotifications()
        repeat(10) { yield() }
    }

    @Test
    fun acknowledgesOnlyCommits() = runBlocking {
        val effects = Effects<String>()
        val committed = mutableListOf<String>()
        val gate = CompletableDeferred<Unit>()
        effects.post("Removed")

        // Cancelled before its commit (a rotation): the head stays queued.
        var host = launch { effects.drain { gate.await(); committed += it } }
        settle()
        host.cancelAndJoin()
        assertEquals("Removed", effects.head?.effect)
        assertEquals(emptyList<String>(), committed)

        // Run again: committed once, acknowledged once.
        gate.complete(Unit)
        host = launch { effects.drain { committed += it } }
        settle()
        assertEquals(listOf("Removed"), committed)
        assertEquals(null, effects.head)

        // A committed head isn't run again after a restart; an equal effect posted again is a new one.
        host.cancelAndJoin()
        host = launch { effects.drain { committed += it } }
        settle()
        effects.post("Removed")
        settle()
        assertEquals(listOf("Removed", "Removed"), committed)
        host.cancelAndJoin()
    }

    @Test
    fun aThrowingEffectIsDropped() = runBlocking {
        val effects = Effects<String>()
        val handled = mutableListOf<String>()
        val failures = mutableListOf<Exception>()
        effects.post("Bad")
        effects.post("Good")
        val host = launch {
            effects.drain({ failures += it }) { if (it == "Bad") error("boom") else handled += it }
        }
        settle()
        assertEquals(listOf("Good"), handled)
        assertEquals(1, failures.size)
        assertEquals(null, effects.head)
        host.cancelAndJoin()
    }
}
