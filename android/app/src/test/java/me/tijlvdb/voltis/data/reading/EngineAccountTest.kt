package me.tijlvdb.voltis.data.reading

import kotlinx.coroutines.CompletableDeferred
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * readingSync.test.ts, `account switch`: a change of account stops the engine (the web's `freeze()`),
 * and the page reload is a new engine over the account's store when it signs in again.
 */
class EngineAccountTest : EngineTest() {
    @Test
    fun `freezes, ignores late responses and reloads the page`() = engineTest {
        server.set("c") { copy(status = "on_hold") }
        val (sync) = open("c")
        server.holding = true
        read(sync, 1)
        sync.moved(page(2))
        engine.stop()
        server.release()
        advance(5000)
        assertEquals(listOf("position"), ops())
        assertNull(prompt())
        // Both stay for the account's next engine: the one out with its seq, and the one after.
        assertEquals(listOf(page(1) to true, page(2) to false), store.ops.map { it.payload to (it.sentSeq != null) })
    }

    @Test
    fun `stops at sign-out, rejecting what waits, and reloads at the next sign-in`() = engineTest {
        val (sync) = open("c")
        server.holding = true
        val out = CompletableDeferred<Result<CommandOutcome>>()
        later { out.complete(runCatching { sync.command(op("set_status", "status" to "reading")) }) }
        settle()
        val waiting = CompletableDeferred<Result<CommandOutcome>>()
        later { waiting.complete(runCatching { sync.command(op("mark_completed")) }) }
        settle()
        engine.stop()
        // Not the web's rejection: the ops stay, and the next engine sends them. The request out is still held: its caller doesn't wait for it.
        assertEquals(CommandOutcome.QUEUED, out.getCompleted().getOrThrow())
        assertEquals(CommandOutcome.QUEUED, waiting.getCompleted().getOrThrow())
        assertEquals(2, store.ops.size)
        val late = attach("d")
        settle()
        assertTrue(runCatching { late.loading.await() }.isFailure)
        restart()
        server.release()
        settle()
        assertEquals(emptyList<Any>(), store.ops)
        assertEquals("completed", server.stateOf("c").status)
    }

    @Test
    fun `ignores a check out at the switch`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(1)) }
        val (sync, placements) = open("c")
        server.holding = true
        sync.check()
        settle()
        server.set("c") { copy(progress = page(8)) }
        engine.stop()
        server.release()
        settle()
        assertEquals(emptyList<Any>(), placements)
        assertEquals(page(1), sync.view.value.acked?.progress)
    }

    /** Not in the web: after `NeedsReauth` or a sign-out that kept the data, the same account's outbox goes on. */
    @Test
    fun `continues the outbox when the same account signs in again`() = engineTest {
        val (sync) = open("c")
        server.unreachable = true
        read(sync, 3)
        server.unreachable = false
        restart()
        assertEquals(page(3), server.stateOf("c").progress)
        assertEquals(emptyList<Any>(), store.ops)
    }
}
