package me.tijlvdb.voltis.data.reading

import kotlin.coroutines.CoroutineContext
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.async
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.domain.reading.EmptyProgress
import me.tijlvdb.voltis.domain.reading.PromptChoice
import me.tijlvdb.voltis.domain.reading.ReadingSession
import me.tijlvdb.voltis.domain.reading.SyncNotice
import me.tijlvdb.voltis.domain.reading.Undoable
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** readingSync.test.ts, `reading out when something replaces it`. */
class EngineReplacedTest : EngineTest() {
    /** A save of p.4 that's out when [replace] runs, and fails after: it goes nowhere again. */
    private suspend fun EngineTest.Harness.failsAfter(setup: suspend () -> ReadingSession, replace: suspend (ReadingSession) -> Unit): ReadingSession {
        val sync = setup()
        server.holding = true
        read(sync, 4)
        settle()
        val replaced = testScope.async { replace(sync) }
        settle()
        server.refuse = { it.body?.string("op") == "position" }
        server.release()
        replaced.await()
        settle()
        server.refuse = { false }
        assertEquals(0, sync.view.value.failures)
        sync.retry()
        advance(2000)
        return sync
    }

    @Test
    fun `invalidateRestores drops a restore already posted, and the session sets the page count`() = engineTest {
        val main = HeldMain()
        engine.stop()
        engine = newEngine(main = main).also { it.start() }
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync, placements) = open("c")
        server.set("c") { copy(progress = page(53)) }
        sync.check()
        settle()
        // Followed, with its restore waiting for the main thread.
        notice<SyncNotice.Followed>()
        sync.invalidateRestores()
        main.run()
        settle()
        assertEquals(emptyList<Any>(), placements)

        sync.pageCount(42)
        settle()
        assertEquals(42, store.lane("c")?.pageCount)
        assertEquals(emptyList<Any>(), writes())
    }

    /** A main thread that runs only when told. */
    private class HeldMain : CoroutineDispatcher() {
        private val queue = ArrayDeque<Runnable>()

        override fun dispatch(context: CoroutineContext, block: Runnable) {
            queue += block
        }

        fun run() {
            while (queue.isNotEmpty()) queue.removeFirst().run()
        }
    }

    private fun EngineTest.Harness.positionsAfter(op: String) = ops().let { all -> all.drop(all.lastIndexOf(op) + 1).filter { it == "position" } }

    @Test
    fun `is dropped for a clear`() = engineTest {
        failsAfter({ open("c").sync }) { it.command(op("clear")) }
        assertEquals(emptyList<Any>(), positionsAfter("clear"))
        assertNull(server.stateOf("c").status)
        assertEquals(EmptyProgress, server.stateOf("c").progress)
    }

    @Test
    fun `is dropped for Mark completed, and stays dropped through the next command`() = engineTest {
        val sync = failsAfter({ open("c").sync }) { it.command(op("mark_completed")) }
        sync.command(op("set_status", "status" to "reading"))
        settle()
        assertEquals(emptyList<Any>(), positionsAfter("mark_completed"))
        assertEquals("reading", server.stateOf("c").status)
        assertEquals(JsonPrimitive(true), server.stateOf("c").progress["at_end"])
    }

    @Test
    fun `is dropped for an Undo`() = engineTest {
        server.set("c") { copy(status = "on_hold") }
        failsAfter(
            {
                val (sync) = open("c")
                read(sync, 1)
                sync
            },
        ) { offer(Undoable.MOVED_TO_READING).action!!() }
        assertEquals(emptyList<Any>(), positionsAfter("restore"))
        assertEquals("on_hold", server.stateOf("c").status)
    }

    @Test
    fun `is dropped for a series completion that covers it`() = engineTest {
        server.series("s", "c")
        failsAfter({ open("c").sync }) {
            it.completeSeries()
            settle()
            answer(PromptChoice.CONFIRM_WITH_UNREAD)
        }
        assertEquals("completed", server.stateOf("c").status)
        assertEquals(JsonPrimitive(true), server.stateOf("c").progress["at_end"])
        assertEquals(1, ops().count { it == "position" })
    }
}
