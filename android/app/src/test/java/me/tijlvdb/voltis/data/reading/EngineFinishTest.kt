package me.tijlvdb.voltis.data.reading

import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.domain.reading.ConflictKind
import me.tijlvdb.voltis.domain.reading.EmptyProgress
import me.tijlvdb.voltis.domain.reading.PromptChoice
import me.tijlvdb.voltis.domain.reading.SyncPrompt
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

/** readingSync.test.ts, `finish`. */
class EngineFinishTest : EngineTest() {
    @Test
    fun `is taken once, and reports at the finished position are not reading on`() = engineTest {
        val (sync) = open("c")
        sync.finish(END)
        sync.moved(page(LAST))
        sync.finish(END)
        advance(2000)
        assertEquals(listOf("finish"), ops())
        assertEquals(JsonPrimitive(true), server.stateOf("c").progress["at_end"])
    }

    /** The finish acknowledged, and still out. */
    @Test
    fun `reads a turn back right after finishing`() = eachCase(listOf(false, true)) { turnBack(stillOut = it) }

    private fun turnBack(stillOut: Boolean) = engineTest {
        val (sync) = open("c")
        server.holding = stillOut
        sync.finish(END)
        settle()
        sync.moved(page(LAST - 1))
        sync.setVisible(false)
        server.release()
        settle()
        assertEquals(listOf("finish", "position"), ops())
        assertEquals("completed" to page(LAST - 1), server.stateOf("c").let { it.status to it.progress })
    }

    /** The positions acknowledged, and still queued. */
    @Test
    fun `leaves a completed item read again at its bookmark`() = eachCase(listOf(true, false)) { bookmark(acknowledged = it) }

    private fun bookmark(acknowledged: Boolean) = engineTest {
        server.set("c") { copy(status = "completed", progress = END) }
        val (sync, _, opened) = open("c")
        assertEquals(JsonPrimitive(true), opened["at_end"])
        sync.moved(page(3))
        if (acknowledged) advance(1500)
        sync.moved(page(LAST))
        if (acknowledged) advance(1500)
        sync.finish(END)
        settle()
        // Acknowledged, the finish isn't sent; queued, the server makes it nothing.
        assertEquals(if (acknowledged) "position" else "finish", ops().last())
        assertEquals("completed" to page(LAST), server.stateOf("c").let { it.status to it.progress })
        assertFalse("at_end" in server.stateOf("c").progress)

        sync.detach()
        assertEquals(page(LAST), open("c").opened)
    }

    @Test
    fun `is taken again once set back to Reading`() = engineTest {
        val (sync) = open("c")
        sync.finish(END)
        settle()
        sync.command(op("set_status", "status" to "reading"))
        sync.finish(END)
        settle()
        assertEquals(2, ops().count { it == "finish" })
    }

    // A one-page comic: the finish, the place it is dropped for and the place after are one page.
    @Test
    fun `is taken again once a clear drops a failed finish`() = engineTest {
        val (sync) = open("c")
        server.failing = true
        sync.finish(END)
        settle()
        server.failing = false
        sync.command(op("clear"))
        sync.placed(page(LAST))
        sync.finish(END)
        settle()
        assertEquals(listOf("finish", "clear", "finish"), ops())
        assertEquals("completed", server.stateOf("c").status)
    }

    @Test
    fun `survives a conflict the reader stays through`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync) = open("c")
        server.set("c") { copy(progress = page(5)) }
        sync.finish(END)
        settle()
        answer(PromptChoice.STAY)
        settle()
        assertEquals("completed", server.stateOf("c").status)
    }

    /** The clear made here, and elsewhere. */
    @Test
    fun `is taken again after a clear`() = eachCase(listOf(true, false)) { clearedThenFinished(here = it) }

    private fun clearedThenFinished(here: Boolean) = engineTest {
        val (sync, placements) = open("c")
        sync.finish(END)
        settle()
        if (here) {
            sync.resetAndReadAgain()
            settle()
            answer(PromptChoice.CONFIRM)
        } else {
            server.clear("c")
            sync.check()
            settle()
            assertEquals(ConflictKind.RESET, (prompt() as SyncPrompt.Conflict).kind)
            answer(PromptChoice.START)
        }
        assertEquals(listOf(EmptyProgress), placements)
        sync.finish(END)
        settle()
        assertEquals("completed", server.stateOf("c").status)
    }

    @Test
    fun `reads a deliberate turn after a placement back from the end`() = engineTest {
        val (sync) = open("c")
        sync.finish(END)
        settle()
        sync.placed(page(0))
        read(sync, 1)
        assertEquals(page(1), server.stateOf("c").progress)
    }

    @Test
    fun `gets the series as completion left it`() = engineTest {
        server.series("s", "c", "d")
        server.set("d") { copy(status = "completed") }
        server.set("s") { copy(status = "reading") }
        val (sync) = open("c")
        sync.finish(END)
        settle()
        val series = checkNotNull(sync.view.value.series)
        assertEquals(true to 2, series.caughtUp to series.completedChildrenCount)
    }
}
