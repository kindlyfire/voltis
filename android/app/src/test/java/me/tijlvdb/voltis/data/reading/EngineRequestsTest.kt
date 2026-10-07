package me.tijlvdb.voltis.data.reading

import me.tijlvdb.voltis.domain.reading.OpKind
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** readingSync.test.ts, `requests`. */
class EngineRequestsTest : EngineTest() {
    @Test
    fun `sends one at a time, in order, with each base the one before acknowledged`() = engineTest {
        val (sync) = open("c")
        server.holding = true
        sync.moved(page(1))
        sync.finish(END)
        later { sync.command(op("set_status", "status" to "reading")) }
        sync.check()
        settle()
        assertEquals(1, server.inFlight)
        repeat(4) {
            server.release()
            server.holding = true
            settle()
        }
        server.release()
        settle()
        // The check goes once the request out settles, ahead of the rest.
        assertEquals(listOf("get", "position", "get", "finish", "set_status"), server.sent.map { it.body?.string("op") ?: it.path })
        val sent = writes()
        assertEquals("${sent[0].string("writer_id")}:${sent[0].long("seq")}", sent[1].string("base_revision"))
        assertEquals("${sent[1].string("writer_id")}:${sent[1].long("seq")}", sent[2].string("base_revision"))
    }

    @Test
    fun `writes nothing for placements`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(4)) }
        val (sync) = open("c")
        sync.placed(page(7))
        sync.placed(page(0))
        advance(5000)
        assertEquals(emptyList<Any>(), writes())
    }

    @Test
    fun `coalesces positions, but not across a finish or command`() = engineTest {
        val (sync) = open("c")
        sync.moved(page(1))
        sync.moved(page(2))
        sync.finish(END)
        settle()
        sync.moved(page(3))
        sync.moved(page(4))
        sync.command(op("set_status", "status" to "on_hold"))
        assertEquals(
            listOf("position" to 2, "finish" to LAST, "position" to 4, "set_status" to null),
            writes().map { it.string("op") to it.progress()?.int("current_page") },
        )
    }

    @Test
    fun `ends a run of positions at a placement`() = engineTest {
        val (sync) = open("c")
        server.holding = true
        later { sync.command(op("set_status", "status" to "reading")) }
        settle()
        sync.moved(page(4))
        sync.placed(page(20))
        sync.moved(page(21))
        server.release()
        advance(1000)
        assertEquals(listOf(4, 21), writes().mapNotNull { it.progress()?.int("current_page") })
    }

    @Test
    fun `keeps reading made while an earlier write was out`() = engineTest {
        val (sync) = open("c")
        server.holding = true
        read(sync, 4)
        read(sync, 5)
        server.release()
        settle()
        advance(1000)
        assertEquals(page(5), server.stateOf("c").progress)
    }

    @Test
    fun `sends a lost acknowledgement’s successor on top of it, without a dialog`() = engineTest {
        val (sync) = open("c")
        server.loseAck = true
        read(sync, 1)
        assertEquals(page(1), server.stateOf("c").progress)
        read(sync, 2)
        assertEquals(page(2), server.stateOf("c").progress)
        assertNull(prompt())
        assertEquals(0, sync.view.value.failures)
        server.loseAck = true
        read(sync, 3)
        sync.command(op("mark_completed"))
        assertEquals("completed", server.stateOf("c").status)
    }

    /** Not in the web: a seq that can't be reserved is a failed send that leaves the op queued, unsent (P2 §5, Sending). */
    @Test
    fun `keeps an op queued when no seq can be reserved`() = engineTest {
        val (sync) = open("c")
        writer.broken = true
        read(sync, 1)
        assertEquals(emptyList<Any>(), writes())
        assertEquals(listOf(OpKind.POSITION to null), store.ops.map { it.kind to it.sentSeq })
        assertEquals(1, sync.view.value.failures)
        writer.broken = false
        sync.retry()
        settle()
        assertEquals(page(1), server.stateOf("c").progress)
        assertEquals(emptyList<Any>(), store.ops)
    }
}
