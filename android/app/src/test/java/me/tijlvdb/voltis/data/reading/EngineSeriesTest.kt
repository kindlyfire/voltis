package me.tijlvdb.voltis.data.reading

import kotlinx.coroutines.async
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.content.withReading
import me.tijlvdb.voltis.domain.catalog.VolumeReading
import me.tijlvdb.voltis.domain.catalog.offlineContinue
import me.tijlvdb.voltis.domain.reading.Change
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.reading.EmptyProgress
import me.tijlvdb.voltis.domain.reading.Lane
import me.tijlvdb.voltis.domain.reading.Op
import me.tijlvdb.voltis.domain.reading.OpKind
import me.tijlvdb.voltis.domain.reading.PromptChoice
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.ReadingSession
import me.tijlvdb.voltis.domain.reading.ReadingSnapshot
import me.tijlvdb.voltis.domain.reading.ReadingState
import me.tijlvdb.voltis.domain.reading.SeriesAction
import me.tijlvdb.voltis.domain.reading.SyncAction
import me.tijlvdb.voltis.domain.reading.SyncNotice
import me.tijlvdb.voltis.domain.reading.SyncPrompt
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * readingSync.test.ts, `series commands` and `series completion`; then what the web's pages do
 * with plain requests and the engine does as ops: `mark_through` and a series clear.
 */
class EngineSeriesTest : EngineTest() {
    private val series = Content("s", "Tin Lantern", ContentType.COMIC_SERIES)

    @Test
    fun `shows a queued series status at once, and adopts the series as the server has it after each read`() = engineTest {
        server.series("s", "c")
        server.set("s") { copy(status = "on_hold") }
        val (sync) = open("c")
        server.unreachable = true
        assertEquals(CommandOutcome.QUEUED, sync.seriesCommand("reading"))
        // The series has no lane of its own: it is shown from what the reader's lane knows of it.
        assertEquals("reading", sync.view.value.series?.status)
        server.unreachable = false
        engine.drain()
        assertEquals("reading" to "reading", server.stateOf("s").status to sync.view.value.series?.status)
        server.set("s") { copy(status = "dropped") }
        sync.check()
        settle()
        assertEquals("dropped", sync.view.value.series?.status)

        // An older page that says Reading already: the series' lane is made from the newest state the server stated (the snapshot), not from the page.
        engine.seed(listOf(series.copy(userData = UserData(status = "reading", revision = "srv:old"))))
        server.unreachable = true
        // Reading that doesn't start the series leaves it as the reader last read it, not as that lane has it.
        read(sync, 2)
        assertEquals("dropped", sync.view.value.series?.status)
        assertEquals(CommandOutcome.QUEUED, sync.seriesCommand("reading"))
        assertEquals("dropped" to "reading", store.lane("s")?.state?.status to sync.view.value.series?.status)
    }

    @Test
    fun `asks, then completes in turn, adopting its own write to the item`() = engineTest {
        server.series("s", "c", "d")
        server.set("s") { copy(status = "reading") }
        val (sync) = open("c")
        server.holding = true
        read(sync, 3)
        sync.completeSeries()
        settle()
        assertEquals(SyncPrompt.ConfirmCompleteSeries(unread = 2), prompt())
        answer(PromptChoice.CONFIRM_WITH_UNREAD)
        server.release()
        settle()
        assertEquals("completed", server.stateOf("d").status)
        assertEquals("completed", sync.view.value.acked?.status)
        assertEquals("completed", sync.view.value.series?.status)
        assertNull(prompt())
        assertEquals(SyncNotice.Done(SyncAction.MARK_SERIES_COMPLETED), notices.last().notice)
    }

    private fun EngineTest.Harness.failTwice(sync: ReadingSession) {
        server.failing = true
        read(sync, 3)
        read(sync, 4)
        server.failing = false
    }

    /** Unread volumes too, and the series alone. */
    @Test
    fun `settles failed reading first, never replayed over it`() = eachCase(listOf(true, false)) { settlesFailedReading(includeUnread = it) }

    private fun settlesFailedReading(includeUnread: Boolean) = engineTest {
        server.series("s", "c")
        val (sync) = open("c")
        failTwice(sync)
        sync.completeSeries()
        settle()
        answer(if (includeUnread) PromptChoice.CONFIRM_WITH_UNREAD else PromptChoice.CONFIRM)
        settle()
        sync.retry()
        advance(2000)
        assertEquals("completed", server.stateOf("s").status)
        // Completed with the series, or saved before it.
        val c = server.stateOf("c")
        if (includeUnread) {
            assertEquals("completed" to JsonPrimitive(true), c.status to c.progress["at_end"])
        } else {
            assertEquals("reading" to page(4), c.status to c.progress)
        }
    }

    /** A reading sibling's unsent reading is covered and dropped; a completed one's is kept. */
    @Test
    fun `drops a sibling volume's failed reading only where the completion covers it`() =
        eachCase(listOf("reading" to true, "completed" to false)) { (status, covered) -> siblingsFailedReading(status, covered) }

    private fun siblingsFailedReading(status: String, covered: Boolean) = engineTest {
        server.series("s", "c", "d")
        server.set("d") { copy(status = status, progress = page(1)) }
        val d = open("d")
        failTwice(d.sync)
        d.sync.detach()
        val (sync) = open("c")
        sync.completeSeries()
        settle()
        answer(PromptChoice.CONFIRM_WITH_UNREAD)
        settle()
        val again = open("d")
        again.sync.retry()
        advance(2000)
        val after = server.stateOf("d")
        assertEquals("completed", after.status)
        if (covered) assertEquals(JsonPrimitive(true), after.progress["at_end"]) else assertEquals(page(4), after.progress)
    }

    @Test
    fun `mark_through completes the volumes up to one, dropping their unsent reading, and seeds them`() = engineTest {
        server.series("s", "a", "b", "c")
        server.set("b") { copy(status = "reading", progress = page(1)) }
        val b = open("b")
        failTwice(b.sync)
        b.sync.detach()
        settle()

        val missing = runCatching { engine.seriesCommand(series, SeriesAction.MARK_THROUGH, untilId = "x") }.exceptionOrNull()
        assertEquals("Volume not found", (missing as ReadingFailure.Refused).message)
        assertTrue(store.ops.none { it.kind == OpKind.SERIES })

        val revision = server.stateOf("b").revision
        server.holding = true
        server.holds = { it.path == "series-reading" }
        val command = testScope.async { engine.seriesCommand(series, SeriesAction.MARK_THROUGH, untilId = "b") }
        settle()
        // Made against what the server listed, with the reading it replaces gone.
        val op = store.ops.single()
        val guard = JsonObject(mapOf("series" to JsonNull, "volumes" to JsonObject(mapOf("a" to JsonNull, "b" to JsonPrimitive(revision)))))
        assertEquals(listOf(OpKind.SERIES, "s", guard, listOf("s")), listOf(op.kind, op.contentId, op.guard, op.after))
        val body = server.sent.last().body!!
        assertEquals(setOf("action", "until_id", "ids", "writer_id", "seq"), body.keys)
        assertEquals("mark_through" to "b", body.string("action") to body.string("until_id"))
        // It sends the volumes it guarded, so the receipt covers them.
        assertEquals(listOf("a", "b"), (body["ids"] as JsonArray).map { (it as JsonPrimitive).content })

        // The answer's states are stored with the op's retiring: while that commit waits, neither is.
        faults.hold = { it.deleteOps.isNotEmpty() }
        server.release()
        settle()
        assertEquals(listOf(true, false), listOf(store.ops.isNotEmpty(), store.snapshot("a")?.seq == server.stateOf("a").seq))
        faults.release()
        assertEquals(CommandOutcome.APPLIED, command.await())
        settle()
        assertEquals(listOf("a", "b", "c", "s").map { server.stateOf(it) }, listOf("a", "b", "c", "s").map { store.snapshot(it) })
        assertEquals(emptyList<Any>(), store.ops)
        assertEquals(listOf("completed", "completed", null, "reading"), listOf("a", "b", "c", "s").map { server.stateOf(it).status })
        assertEquals(1, server.sent.count { it.path == "series-reading" })
        // Every listed volume has a lane, as the server left it.
        assertEquals(listOf("a", "b", "c").map { server.stateOf(it) }, listOf("a", "b", "c").map { store.lane(it)?.state })
        assertEquals("s", store.lane("a")?.parentId)
        assertEquals("reading", store.lane("s")?.state?.status)
        engine.drain()
        assertEquals(JsonPrimitive(true), server.stateOf("b").progress["at_end"])
    }

    /** The whole path of one offline Continue: an acknowledged clear, a restart, a prune and a stale fetch, with another volume left as the target. */
    @Test
    fun `offline continue keeps its target through a clear, a restart, a prune and a stale fetch`() = engineTest {
        server.series("s", "a", "b", "c")
        server.set("a") { copy(status = "reading", progress = page(3), lastReadAt = "2026-01-05T00:00:00Z") }
        server.set("b") { copy(status = "reading", progress = page(2), lastReadAt = "2026-01-03T00:00:00Z") }
        val stale = fetched("a")
        store.cache(listOf(fetched("s", ContentType.COMIC_SERIES) to null) + listOf("a", "b", "c").mapIndexed { i, id -> fetched(id) to i }, wholeList = "s")
        suspend fun target(): String? {
            val reading = store.effective(listOf("a", "b", "c"))
            return offlineContinue("s", listOf("a", "b", "c").map { VolumeReading(fetched(it), reading.getValue(it)) }).target?.id
        }
        assertEquals("a", target())

        // A is cleared, and the answer acknowledged: B was read last.
        assertEquals(CommandOutcome.APPLIED, engine.command(fetched("a"), op("clear")))
        settle()
        assertEquals("b", target())
        restart()
        store.prune(before = Long.MAX_VALUE)
        assertEquals("b", target())
        // A list fetched before the clear arrives late, as a row and as a seed: it is older than what the phone has.
        store.cache(listOf(stale to 0), wholeList = "s")
        engine.seed(listOf(stale))
        assertEquals("b", target())
    }

    /** An offline page is a display, never a seed: its reading and revision are the snapshot's. A series cleared from it sends what the snapshot says. */
    @Test
    fun `a series command from an overlaid page seeds from the snapshot, its lane pruned`() = engineTest {
        server.series("s", "a", "b")
        server.set("s") { copy(status = "reading") }
        server.set("a") { copy(status = "completed", progress = END) }
        val cachedSeries = fetched("s", ContentType.COMIC_SERIES)
        store.cache(listOf(cachedSeries to null) + listOf("a", "b").mapIndexed { i, id -> fetched(id) to i }, wholeList = "s")
        // Another device changes the series: the phone learns it from an answer, and the page's row stays as it was fetched.
        server.set("s") { copy(status = "on_hold") }
        store.import(listOf(ReadingSnapshot("s", server.stateOf("s"))))
        restart()
        store.prune(before = Long.MAX_VALUE)
        assertNull(store.lane("s"))
        val page = cachedSeries.withReading(store.effective(listOf("s"))["s"])
        // The overlay carries the snapshot's whole identity, so the page is never a hybrid of two states.
        assertEquals(server.stateOf("s").let { listOf(it.seq, it.revision, it.progressUpdatedAt) }, page.userData.let { listOf(it?.readingSeq, it?.revision, it?.progressUpdatedAt) })

        assertEquals(CommandOutcome.APPLIED, engine.command(page, op("clear")))
        settle()
        assertEquals(null to EmptyProgress, server.stateOf("s").let { it.status to it.progress })
        assertEquals(server.stateOf("s"), store.snapshot("s"))
    }

    @Test
    fun `a series op that landed is applied, though the read after it fails, and its lane is checked again`() = engineTest {
        server.series("s", "a", "b")
        val (sync) = open("a")
        server.refuse = { it.path == "get" }
        sync.completeSeries()
        settle()
        answer(PromptChoice.CONFIRM)
        assertEquals(SyncNotice.Done(SyncAction.MARK_SERIES_COMPLETED), notices.last().notice)
        server.refuse = { false }
        // The next genuine answer, for another item, checks the lane: the reader shows what the op did.
        engine.command(Content("b", "Title b", ContentType.COMIC, parentId = "s"), op("set_status", "status" to "on_hold"))
        settle()
        assertEquals("completed" to "a", sync.view.value.series?.status to server.sent.last { it.path == "get" }.id)
        assertEquals(1, server.sent.count { it.path == "series-reading" })
        assertEquals(emptyList<Any>(), store.ops)
    }

    @Test
    fun `a series clear clears its volumes with it, dropping their unsent reading`() = engineTest {
        server.series("s", "a", "b")
        server.set("s") { copy(status = "reading") }
        server.set("a") { copy(status = "completed", progress = END) }
        server.set("b") { copy(status = "reading", progress = page(1)) }
        val b = open("b")
        failTwice(b.sync)
        b.sync.detach()
        settle()
        val fetched = series.copy(userData = UserData(status = "reading", revision = server.stateOf("s").revision))
        assertEquals(CommandOutcome.APPLIED, engine.command(fetched, op("clear")))
        settle()
        engine.drain()
        for (id in listOf("s", "a", "b")) {
            assertEquals(null to EmptyProgress, server.stateOf(id).let { it.status to it.progress })
            assertEquals(server.stateOf(id), store.lane(id)?.state)
        }
        assertEquals(listOf("position", "position", "clear"), ops())
        // The clear sends the volumes it guarded, and its answer carried their states.
        val clear = server.sent.last { it.id == "s" && it.path == "reading" }.body!!
        assertEquals(listOf("a", "b"), (clear["ids"] as JsonArray).map { (it as JsonPrimitive).content })
        assertEquals(listOf("s", "a", "b").map { server.stateOf(it) }, listOf("s", "a", "b").map { store.snapshot(it) })
    }

    @Test
    fun `a mark_through killed while out and repeated with its seq is not applied twice`() {
        server.series("s", "a", "b")
        server.loseAck = true
        engineTest(
            before = {
                val payload = JsonObject(mapOf("series_id" to JsonPrimitive("s"), "action" to JsonPrimitive("mark_through"), "until_id" to JsonPrimitive("b")))
                val guard = JsonObject(mapOf("series" to JsonNull, "volumes" to JsonObject(mapOf("a" to JsonNull, "b" to JsonNull))))
                val lanes = listOf(Lane("s", type = ContentType.COMIC_SERIES), Lane("a", parentId = "s", state = ReadingState()), Lane("b", parentId = "s"))
                store.commit(Change(lanes = lanes, ops = listOf(Op(contentId = "s", kind = OpKind.SERIES, payload = payload, after = listOf("s"), guard = guard))))
            },
        ) {
            settle()
            // It landed, and its answer was lost. A later write of ours then reads a volume again: the guard
            // lets the repeat go (another device's write would drop it, EngineGuardTest), and the server ignores it.
            assertEquals("completed", server.stateOf("a").status)
            assertEquals(1, store.ops.size)
            server.set("a", rev = "${writer.writerId}:${writer.seq + 1}") { copy(status = "reading", progress = page(2)) }
            restart()
            val sent = server.sent.filter { it.path == "series-reading" }.map { it.body!!.long("seq") }
            assertEquals(2, sent.size)
            assertEquals(sent[0], sent[1])
            assertEquals("reading" to page(2), server.stateOf("a").let { it.status to it.progress })
            assertEquals(emptyList<Any>(), store.ops)
            assertEquals("reading", store.lane("a")?.state?.status)
        }
    }
}
