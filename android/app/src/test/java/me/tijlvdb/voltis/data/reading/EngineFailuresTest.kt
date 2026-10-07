package me.tijlvdb.voltis.data.reading

import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.domain.reading.PromptChoice
import me.tijlvdb.voltis.domain.reading.SyncAction
import me.tijlvdb.voltis.domain.reading.SyncPrompt
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * readingSync.test.ts, `failures`: the web's `offline` is [me.tijlvdb.voltis.domain.reading.FakeReadingServer.failing],
 * a server error with the reader attached.
 */
class EngineFailuresTest : EngineTest() {
    @Test
    fun `retries a failed save on Retry or new reading, never on focus`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(0)) }
        val (sync) = open("c")
        server.failing = true
        read(sync, 3)
        server.failing = false
        // Focus, and the app's foreground drain, which leaves an attached reader's failure to it.
        sync.setVisible(false)
        sync.setVisible(true)
        engine.drain()
        advance(5000)
        assertEquals(page(0), server.stateOf("c").progress)
        sync.retry()
        settle()
        assertEquals(page(3), server.stateOf("c").progress)

        server.failing = true
        read(sync, 4)
        read(sync, 5)
        read(sync, 6)
        assertEquals(3, sync.view.value.failures)
        server.failing = false
        read(sync, 7)
        assertEquals(page(7), server.stateOf("c").progress)
        assertEquals(0, sync.view.value.failures)
    }

    @Test
    fun `replaces blocked reading with Mark completed, and sends it before other commands`() = engineTest {
        val (sync) = open("c")
        server.failing = true
        read(sync, 3)
        server.failing = false
        sync.command(op("mark_completed"))
        sync.retry()
        settle()
        assertEquals(JsonPrimitive(true), server.stateOf("c").progress["at_end"])
        assertEquals(listOf("position", "mark_completed"), ops())

        server.failing = true
        read(sync, 4)
        server.failing = false
        sync.command(op("set_status", "status" to "on_hold"))
        assertEquals("on_hold" to page(4), server.stateOf("c").let { it.status to it.progress })
    }

    @Test
    fun `fails a command with the older reading it would overtake, which Retry still sends`() = engineTest {
        val (sync) = open("c")
        server.refuse = { it.body?.string("op") == "finish" }
        sync.finish(END)
        settle()
        assertTrue(runCatching { sync.command(op("set_status", "status" to "reading")) }.isFailure)
        assertEquals(listOf("finish", "finish"), ops())
        server.refuse = { false }
        sync.retry()
        settle()
        sync.command(op("set_status", "status" to "reading"))
        assertEquals(listOf("finish", "set_status"), ops().takeLast(2))
        assertEquals("reading", server.stateOf("c").status)
    }

    @Test
    fun `offers Retry for a failed finish at once, and keeps it shown until one succeeds`() = engineTest {
        val (sync) = open("c")
        server.failing = true
        sync.finish(END)
        settle()
        assertTrue(sync.view.value.failures >= 3)
        sync.finish(END)
        sync.setVisible(false)
        settle()
        assertEquals(listOf("finish"), ops())
        sync.retry()
        settle()
        assertTrue(sync.view.value.failures >= 3)
        server.failing = false
        sync.retry()
        settle()
        assertEquals(0, sync.view.value.failures)
        assertEquals("completed", server.stateOf("c").status)
        // A command, unlike a save, fails to its caller.
        server.failing = true
        assertTrue(runCatching { sync.command(op("mark_completed")) }.isFailure)
    }

    @Test
    fun `offers Retry when moving the series to Reading fails`() = engineTest {
        server.series("s", "c")
        server.set("s") { copy(status = "on_hold") }
        val (sync) = open("c")
        read(sync, 1)
        assertEquals(SyncPrompt.SeriesHeld("on_hold", null), prompt())
        server.failing = true
        answer(PromptChoice.MOVE)
        server.failing = false
        failure(SyncAction.MOVE_SERIES).action!!()
        settle()
        assertEquals("reading", server.stateOf("s").status)
        assertEquals("reading", sync.view.value.series?.status)
    }
}
