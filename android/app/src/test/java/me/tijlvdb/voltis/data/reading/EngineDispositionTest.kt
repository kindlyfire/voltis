package me.tijlvdb.voltis.data.reading

import me.tijlvdb.voltis.domain.reading.Change
import me.tijlvdb.voltis.domain.reading.DrainResult
import me.tijlvdb.voltis.domain.reading.Lane
import me.tijlvdb.voltis.domain.reading.Op
import me.tijlvdb.voltis.domain.reading.OpKind
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.sync.NoticeKind
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** Not in the web: the answers that end an op, or stop the engine (P2 §5, Results). */
class EngineDispositionTest : EngineTest() {
    @Test
    fun `a 401 keeps the ops through probe, Retry and drain until the engine is replaced`() = engineTest {
        val (sync) = open("c")
        server.failWith = { if (it.body != null) ReadingFailure.SignedOut() else null }
        read(sync, 3)
        connectivity.online.value = false
        connectivity.online.value = true
        sync.retry()
        assertEquals(DrainResult.WAITING, engine.drain())
        assertEquals(1, writes().size)
        assertEquals(listOf(page(3)), store.ops.map { it.payload })
        server.failWith = { null }
        restart()
        assertEquals(page(3), server.stateOf("c").progress)
    }

    @Test
    fun `a write's 404 drops its lane's ops alone, and stores the loss`() = engineTest {
        val (c) = open("c")
        val (d) = open("d")
        server.failWith = { if (it.id == "c" && it.body != null) ReadingFailure.Gone("Content not found") else null }
        server.holding = true
        read(d, 1)
        read(c, 2)
        later { c.command(op("set_status", "status" to "on_hold")) }
        server.release()
        settle()
        assertEquals(emptyList<Any>(), store.ops.filter { it.contentId == "c" })
        assertEquals(listOf("c" to NoticeKind.GONE), store.notices.map { it.contentId to it.kind })
        assertEquals(listOf("c"), gone)
        assertEquals(page(1), server.stateOf("d").progress)
    }

    @Test
    fun `a watched write's 404 also stores the loss of the unwatched ops it drops`() = engineTest {
        val (c) = open("c")
        server.failWith = { if (it.id == "c" && it.body != null) ReadingFailure.Gone("Content not found") else null }
        server.holding = true
        var failure: Throwable? = null
        later { failure = runCatching { c.command(op("set_status", "status" to "on_hold")) }.exceptionOrNull() }
        settle()
        read(c, 2)
        server.release()
        settle()
        assertTrue(failure is ReadingFailure.Gone)
        assertEquals(emptyList<Any>(), store.ops)
        assertEquals(listOf("c" to NoticeKind.GONE), store.notices.map { it.contentId to it.kind })
    }

    @Test
    fun `a reading GET's 404 drops the lane's ops, ends its review and marks its download gone`() {
        engineTest(
            before = {
                // Awaiting review, with reading of its own.
                val reading = Op(contentId = "c", kind = OpKind.POSITION, payload = page(5), sealed = true)
                store.commit(Change(lanes = listOf(Lane("c", title = "Made-up Volume", needsReview = true)), ops = listOf(reading)))
            },
        ) {
            server.failWith = { if (it.path == "get") ReadingFailure.Gone("Content not found") else null }
            val reader = attach("c")
            settle()
            assertTrue(reader.loading.isCompleted)
            assertEquals(emptyList<Any>(), store.ops)
            assertEquals(listOf("c" to NoticeKind.GONE), store.notices.map { it.contentId to it.kind })
            assertEquals(listOf("c"), gone)
            assertEquals(false, store.lane("c")?.needsReview)
            assertEquals(false, reader.sync.view.value.stale)
        }
    }

    @Test
    fun `a refusal goes to the waiting caller, else to a stored notice`() {
        server.failWith = { if (it.body?.string("op") == "set_status") ReadingFailure.Refused(400, "Invalid status") else null }
        engineTest(
            before = {
                val unwatched = Op(contentId = "c", kind = OpKind.COMMAND, payload = op("set_status", "status" to "nonsense"))
                store.commit(Change(lanes = listOf(Lane("c", title = "Made-up Volume")), ops = listOf(unwatched)))
            },
        ) {
            settle()
            assertEquals(listOf(Triple("c", NoticeKind.REFUSED, "Invalid status")), store.notices.map { Triple(it.contentId, it.kind, it.detail.message) })
            val (sync) = open("d")
            assertTrue(runCatching { sync.command(op("set_status", "status" to "nonsense")) }.exceptionOrNull() is ReadingFailure.Refused)
            assertEquals(1, store.notices.size)
            assertEquals(emptyList<Any>(), store.ops)
        }
    }
}
