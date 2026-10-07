package me.tijlvdb.voltis.data.reading

import kotlinx.coroutines.async
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.domain.reading.Change
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.reading.DrainResult
import me.tijlvdb.voltis.domain.reading.EmptyProgress
import me.tijlvdb.voltis.domain.reading.Lane
import me.tijlvdb.voltis.domain.reading.Op
import me.tijlvdb.voltis.domain.reading.OpKind
import me.tijlvdb.voltis.domain.reading.PositionLabel
import me.tijlvdb.voltis.domain.reading.SyncNotice
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Not in the web: the classes of failure, the parked pump and the drain (P2 §5, Results, Opening). */
class EngineOfflineTest : EngineTest() {
    private fun positionsSent(id: String) = server.sent.filter { it.id == id && it.body?.string("op") == "position" }

    @Test
    fun `parks on an unreachable server without counting failures, and merges the positions made meanwhile`() = engineTest {
        val (sync) = open("c")
        server.unreachable = true
        for (n in 1..5) read(sync, n)
        assertEquals(0, sync.view.value.failures)
        // "Saved on this device".
        assertTrue(sync.view.value.run { unsent && parked })
        assertEquals(1, positionsSent("c").size)
        // The first went out, and its result is unknown: the rest merge into the next one.
        assertEquals(listOf(page(1), page(5)), store.ops.map { it.payload })
        assertEquals(false, connectivity.online.value)
        assertTrue(scheduler.scheduled > 0)
        assertTrue(notices.none { it.notice is SyncNotice.Failed })

        server.unreachable = false
        assertEquals(DrainResult.DONE, engine.drain())
        assertEquals(page(5), server.stateOf("c").progress)
        assertEquals(3, positionsSent("c").size)
        assertFalse(sync.view.value.run { unsent || parked })
    }

    @Test
    fun `backs off while the probe passes but the server's answers don't come`() = engineTest {
        connectivity.reachable = { true }
        val (sync) = open("c")
        server.unreachable = true
        read(sync, 1)
        advance(60_000)
        // At once, then again after the passing probe, then 5, 10 and 20 s apart.
        assertEquals(5, positionsSent("c").size)
    }

    @Test
    fun `answers Queued when parked, on a held lane and behind an unknown result`() {
        server.series("s", "a", "b")
        server.refuse = { it.id == "a" }
        engineTest(
            before = {
                val unknown = Op(contentId = "a", kind = OpKind.POSITION, payload = page(1), sealed = true, sentSeq = 3, sentWriter = writer.writerId)
                store.commit(Change(lanes = listOf(Lane("a", parentId = "s")), ops = listOf(unknown)))
            },
        ) {
            settle()
            val (b) = open("b")
            assertEquals(CommandOutcome.QUEUED, b.command(op("set_status", "status" to "on_hold")))

            server.set("c") { copy(status = "reading", progress = page(2)) }
            val (c) = open("c")
            c.moved(page(5))
            server.set("c") { copy(progress = page(79)) }
            c.setVisible(false)
            settle()
            assertEquals(true, c.view.value.stale)
            assertEquals(CommandOutcome.QUEUED, c.command(op("set_status", "status" to "on_hold")))

            val (d) = open("d")
            server.unreachable = true
            read(d, 1)
            assertEquals(CommandOutcome.QUEUED, d.command(op("set_status", "status" to "on_hold")))
            assertEquals(6, store.ops.size)
        }
    }

    @Test
    fun `starts with a drain pass that clears stored failed flags`() = engineTest(
        before = {
            val op = Op(contentId = "c", kind = OpKind.POSITION, payload = page(4), sealed = true)
            store.commit(Change(lanes = listOf(Lane("c", failed = true, failures = 3)), ops = listOf(op)))
        },
    ) {
        settle()
        assertEquals(page(4), server.stateOf("c").progress)
        assertEquals(false to 0, store.lane("c")!!.let { it.failed to it.failures })
    }

    @Test
    fun `unparks on a passing probe or a genuine answer, sending in order`() = engineTest {
        val (c) = open("c")
        val (d) = open("d")
        server.unreachable = true
        read(c, 1)
        read(d, 2)
        server.unreachable = false
        // Another request's genuine answer, or a probe that passed.
        connectivity.online.value = true
        settle()
        assertEquals(listOf("c" to page(1), "c" to page(1), "d" to page(2)), server.sent.filter { it.body != null }.map { it.id to it.body!!.progress() })

        server.unreachable = true
        read(c, 3)
        server.unreachable = false
        // A reader's first read goes while parked, and its answer unparks.
        open("e")
        settle()
        assertEquals(page(3), server.stateOf("c").progress)
    }

    @Test
    fun `opens an acknowledged lane at once when parked, and checks it once the server answers`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        open("c").sync.detach()
        val (d) = open("d")
        server.unreachable = true
        read(d, 1)
        server.set("c") { copy(progress = page(30)) }
        val reader = attach("c")
        settle()
        assertEquals(page(2), reader.loading.getCompleted())
        assertEquals(1, server.sent.count { it.id == "c" && it.path == "get" })

        // Reached again, but with a server error, and offline again before that is seen: the lane is still to be
        // checked, and the connection's return checks it.
        assertFalse(connectivity.online.value)
        server.unreachable = false
        server.failing = true
        connectivity.failsAfterAnswer = true
        attach("x")
        settle()
        connectivity.failsAfterAnswer = false
        server.failing = false
        assertEquals(emptyList<Any>(), reader.placements)
        connectivity.online.value = true
        settle()
        assertEquals(listOf(page(30)), reader.placements)
        assertEquals(SyncNotice.Followed(PositionLabel.Page(31)), notice<SyncNotice.Followed>().notice)

        // A server error on the open's read: the same, and the next genuine answer checks the lane.
        reader.sync.detach()
        server.set("c") { copy(progress = page(35)) }
        server.failing = true
        val again = attach("c")
        settle()
        assertEquals(page(30), again.loading.getCompleted())
        // Server errors meanwhile start no check, and one on a check leaves the lane still to be checked.
        read(d, 2)
        val checks = server.sent.count { it.id == "c" && it.path == "get" }
        connectivity.online.value = false
        settle()
        connectivity.online.value = true
        settle()
        assertEquals(checks + 1, server.sent.count { it.id == "c" && it.path == "get" })
        assertEquals(emptyList<Any>(), again.placements)
        server.failing = false
        read(d, 3)
        assertEquals(listOf(page(35)), again.placements)
    }

    @Test
    fun `opens offline where a seed of another device's reading left it, and sends the next page on top of that`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync) = open("c")
        read(sync, 12)
        sync.detach()
        settle()
        server.set("c") { copy(progress = page(30)) }
        val fetched = server.stateOf("c")
        val row = Content(
            "c", "c", ContentType.COMIC,
            userData = UserData(status = fetched.status, progress = fetched.progress, revision = fetched.revision, readingSeq = fetched.seq),
        )
        engine.seed(listOf(row))
        connectivity.online.value = false
        val reads = server.sent.count { it.path == "get" }
        val reader = attach("c")
        settle()
        assertEquals(page(30), reader.loading.getCompleted())
        assertEquals(reads, server.sent.count { it.path == "get" })
        read(reader.sync, 31)
        assertEquals(fetched.revision, writes().last().string("base_revision"))
        assertEquals(page(31), server.stateOf("c").progress)
        assertNull(prompt())
    }

    @Test
    fun `reads an unacknowledged lane while parked, and its reader's Retry recovers`() = engineTest {
        val (c) = open("c")
        server.unreachable = true
        read(c, 1)
        val reader = attach("x")
        settle()
        assertEquals(1, server.sent.count { it.id == "x" && it.path == "get" })
        assertTrue(runCatching { reader.loading.getCompleted() }.isFailure)

        server.unreachable = false
        val retried = testScope.async { reader.sync.load() }
        settle()
        assertEquals(EmptyProgress, retried.getCompleted())
        assertEquals(page(1), server.stateOf("c").progress)
    }

    @Test
    fun `answers a load waiting when the pump parks from what is stored`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        open("c").sync.detach()
        val (d) = open("d")
        server.holding = true
        read(d, 1)
        val reader = attach("c")
        settle()
        server.unreachable = true
        server.release()
        settle()
        assertEquals(page(2), reader.loading.getCompleted())
        assertEquals(1, server.sent.count { it.id == "c" && it.path == "get" })
    }

    @Test
    fun `keeps an op nobody watches through server errors, sending it once per drain`() = engineTest(
        before = {
            val command = Op(contentId = "c", kind = OpKind.COMMAND, payload = op("set_status", "status" to "on_hold"), after = listOf("c"))
            store.commit(Change(lanes = listOf(Lane("c")), ops = listOf(command)))
        },
    ) {
        server.failing = true
        settle()
        advance(60_000)
        assertEquals(1, writes().size)
        assertTrue(scheduler.scheduled > 0)
        assertEquals(DrainResult.WAITING, engine.drain())
        assertEquals(2, writes().size)
        assertEquals(1, store.ops.size)
        server.failing = false
        assertEquals(DrainResult.DONE, engine.drain())
        assertEquals("on_hold", server.stateOf("c").status)
    }

    @Test
    fun `fails the lane blocked behind an unwatched op that fails again`() {
        server.series("s", "c", "d")
        engineTest(
            before = {
                val op = Op(contentId = "c", kind = OpKind.POSITION, payload = page(4), sealed = true)
                store.commit(Change(lanes = listOf(Lane("c", parentId = "s")), ops = listOf(op)))
            },
        ) {
            server.refuse = { it.id == "c" }
            settle()
            val (d) = open("d")
            read(d, 3)
            // c's write may have landed: d waits behind it, and sees it fail.
            assertEquals(listOf("c", "c"), server.sent.filter { it.body != null }.map { it.id })
            assertEquals(1, d.view.value.failures)
            server.refuse = { false }
            engine.drain()
            assertEquals(page(4), server.stateOf("c").progress)
            read(d, 4)
            assertEquals(page(4), server.stateOf("d").progress)
        }
    }
}
