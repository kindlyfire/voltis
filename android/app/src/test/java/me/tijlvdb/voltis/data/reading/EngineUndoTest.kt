package me.tijlvdb.voltis.data.reading

import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.domain.reading.OpKind
import me.tijlvdb.voltis.domain.reading.PromptChoice
import me.tijlvdb.voltis.domain.reading.SyncAction
import me.tijlvdb.voltis.domain.reading.SyncNotice
import me.tijlvdb.voltis.domain.reading.SyncPrompt
import me.tijlvdb.voltis.domain.reading.Undoable
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** readingSync.test.ts, `undo`. A toast's Undo is the action of its [SyncNotice.UndoOffer]. */
class EngineUndoTest : EngineTest() {
    @Test
    fun `restores, stops tracking, and works after leaving and reopening`() = engineTest {
        server.series("s", "c")
        server.set("c") { copy(status = "plan_to_read") }
        val first = open("c")
        read(first.sync, 1)
        assertEquals("reading", server.stateOf("s").status)
        first.sync.detach()
        open("d")
        val again = open("c")
        offer(Undoable.MOVED_TO_READING).action!!()
        settle()
        assertEquals("plan_to_read", server.stateOf("c").status)
        assertNull(server.stateOf("s").status)
        assertEquals(false, again.sync.view.value.tracking)
        read(again.sync, 2)
        assertEquals(listOf("position", "restore"), ops())
        again.sync.trackProgress()
        read(again.sync, 3)
        assertEquals("reading", server.stateOf("c").status)
    }

    @Test
    fun `puts back a completed series that starting a volume reopened, alone`() = engineTest {
        server.series("s", "c")
        server.set("s") { copy(status = "completed", statusUpdatedAt = "then") }
        val (sync) = open("c")
        read(sync, 1)
        assertEquals("reading", server.stateOf("s").status)
        offer(Undoable.SERIES_MOVED_TO_READING).action!!()
        settle()
        assertEquals("completed" to "then", server.stateOf("s").let { it.status to it.statusUpdatedAt })
        assertEquals("reading", server.stateOf("c").status)
        assertEquals(true, sync.view.value.tracking)
    }

    @Test
    fun `offers Retry when it fails on the server, and nothing after a conflict`() = engineTest {
        server.set("c") { copy(status = "on_hold") }
        val (sync) = open("c")
        read(sync, 1)
        server.failing = true
        offer(Undoable.MOVED_TO_READING).action!!()
        settle()
        server.failing = false
        failure(SyncAction.UNDO).action!!()
        settle()
        assertEquals("on_hold", server.stateOf("c").status)
        sync.detach()

        // Changed on another device first: Retry would put the old state back over it.
        server.set("d") { copy(status = "on_hold") }
        val d = open("d")
        read(d.sync, 1)
        server.set("d") { copy(status = "dropped") }
        val undo = offer(Undoable.MOVED_TO_READING).action!!
        undo()
        settle()
        assertNull(failure(SyncAction.UNDO).action)
        val sent = server.sent.size
        undo()
        settle()
        assertEquals(sent, server.sent.size)
        assertEquals("dropped", server.stateOf("d").status)
    }

    @Test
    fun `is offered by the held-series prompt instead of a toast`() = engineTest {
        server.series("s", "c")
        server.set("s") { copy(status = "on_hold") }
        server.set("c") { copy(status = "dropped") }
        val (sync) = open("c")
        sync.finish(END)
        settle()
        assertEquals(SyncPrompt.SeriesHeld("on_hold", Undoable.MARKED_COMPLETED), prompt())
        answer(PromptChoice.UNDO)
        assertEquals("dropped", server.stateOf("c").status)
        assertEquals(emptyList<Any>(), notices)
    }

    /** Not in the web: the undo is an op with its snapshot and the lane's epoch, so it outlives the process that offered it. */
    @Test
    fun `is stored with what it restores, and sent by the next engine`() = engineTest {
        server.set("c") { copy(status = "on_hold", progress = page(4), lastReadAt = "then") }
        val (sync) = open("c")
        read(sync, 5)
        server.unreachable = true
        offer(Undoable.MOVED_TO_READING).action!!()
        settle()
        val undo = store.ops.single()
        val snapshot = JsonObject(mapOf("status" to JsonPrimitive("on_hold"), "progress" to page(4), "last_read_at" to JsonPrimitive("then")))
        assertEquals(OpKind.UNDO to JsonObject(mapOf("snapshot" to snapshot, "series" to JsonNull)), undo.kind to undo.payload)
        assertEquals(0, undo.epoch)
        assertEquals(false, store.lane("c")?.tracking)
        // Shown as it will be, while it waits.
        assertEquals("on_hold", sync.view.value.status)
        assertTrue(notices.none { it.notice is SyncNotice.Failed })

        restart { server.unreachable = false }
        assertEquals(Triple("on_hold", page(4), "then"), server.stateOf("c").let { Triple(it.status, it.progress, it.lastReadAt) })
        assertEquals(emptyList<Any>(), store.ops)
    }
}
