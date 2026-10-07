package me.tijlvdb.voltis.data.reading

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.content.CatalogChange.PositionSaved
import me.tijlvdb.voltis.data.content.CatalogChange.ReadingChanged
import me.tijlvdb.voltis.data.content.CatalogSignal
import me.tijlvdb.voltis.data.content.CatalogSignal.BatchEnded
import me.tijlvdb.voltis.data.content.CatalogSignal.Changed
import me.tijlvdb.voltis.domain.reading.Change
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.reading.EmptyProgress
import me.tijlvdb.voltis.domain.reading.Lane
import me.tijlvdb.voltis.domain.reading.Op
import me.tijlvdb.voltis.domain.reading.OpKind
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.ReadingState
import me.tijlvdb.voltis.domain.reading.SeriesAction
import me.tijlvdb.voltis.domain.reading.Shown
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.reading.endProgress
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** Not in the web: content pages' commands through lanes that have no reader (P2 §5, §6). */
class EnginePageCommandTest : EngineTest() {
    @Test
    fun `a status set over another device's wins, on a series and on an item, and the next command goes through`() = engineTest {
        // The first command of each is a bulk batch's: its change is stamped, the rest are ordinary.
        catalog.begin(7)
        for ((id, type) in listOf("s" to ContentType.COMIC_SERIES, "c" to ContentType.COMIC)) {
            server.set(id) { copy(status = "reading", progress = if (id == "c") page(5) else EmptyProgress) }
            val page = fetched(id, type)
            val at = now()
            advance(100)
            // Changed on another device since the page was loaded.
            server.set(id) { copy(status = "on_hold") }
            // Unconfined: the caller resumes inside the answer, so what it sees is what was out when the answer came.
            var atAnswer = emptyList<CatalogSignal>()
            val outcome = withContext(Dispatchers.Unconfined) { engine.command(page, status("dropped"), origin = 7).also { atAnswer = signals.toList() } }
            assertEquals(CommandOutcome.APPLIED, outcome)
            // Before the answer: the effect was already out, stamped.
            assertEquals(Changed(ReadingChanged(id, id), 7), atAnswer.last())
            assertEquals("dropped", server.stateOf(id).status)
            // The 409's state was adopted, not held for review: there is nobody to ask and nothing to lose.
            val lane = store.lane(id)!!
            assertEquals(listOf(false, 1, server.stateOf(id)), listOf(lane.needsReview, lane.foreignEpoch, lane.state))
            // The same page, older than the lane now: the command goes on the lane's state. A null status is sent as null.
            assertEquals(CommandOutcome.APPLIED, engine.command(page, status(null)))
            assertEquals(null, server.stateOf(id).status)
            assertEquals(JsonNull, writes().last()["status"])
        }
        assertEquals(listOf("s", "s", "s", "c", "c", "c"), server.sent.filter { it.body != null }.map { it.id })
        assertEquals(ReadingChanged("c", "c"), changes.last())
        // A reader on another item, reading and leaving while the batch is open: its effects are ordinary.
        server.series("t", "r")
        val (reader) = open("r")
        read(reader, 1)
        read(reader, 2)
        reader.detach()
        settle()
        val readers = signals.filterIsInstance<Changed>().filter { it.change is PositionSaved || (it.change as? ReadingChanged)?.contentId == "r" }
        assertTrue(readers.size >= 2 && readers.all { it.batch == null })
        catalog.end(7, emptyList())
        val stamped = signals.filterIsInstance<Changed>().filter { it.batch == 7L }
        assertEquals(listOf(ReadingChanged("s", "s"), ReadingChanged("c", "c")), stamped.map { it.change })
        // Everything else the engine emitted is ordinary, and the batch's changes come before its end.
        assertTrue(signals.filterIsInstance<Changed>().all { it.batch == 7L || it.batch == null })
        assertEquals(BatchEnded(7, emptyList()), signals.last())
    }

    @Test
    fun `a destructive command that conflicts is dropped, and the lane adopts what was cleared elsewhere`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(5)) }
        val page = fetched("c")
        advance(100)
        server.clear("c")
        val rejected = runCatching { engine.command(page, op("mark_completed")) }.exceptionOrNull()
        assertEquals("mark_completed", (rejected as ReadingFailure.ChangedElsewhere).command)
        val lane = store.lane("c")!!
        assertEquals(listOf(false, server.stateOf("c"), EmptyProgress), listOf(lane.needsReview, lane.state, lane.here))
        assertEquals(emptyList<Any>(), store.ops)
        assertEquals(emptyList<Any>(), store.notices)
        // Made again, against what the lane has now.
        assertEquals(CommandOutcome.APPLIED, engine.command(page, op("mark_completed")))
        assertEquals("completed", server.stateOf("c").status)
    }

    @Test
    fun `a replacing command on a lane awaiting review ends the review, and the lane still opens offline`() {
        val acked = ReadingState("srv:before", "reading", progress = page(2))
        server.set("c") { copy(status = "reading", progress = page(30)) }
        engineTest(
            before = {
                val unsent = Op(contentId = "c", kind = OpKind.POSITION, payload = page(5), sealed = true)
                store.commit(Change(lanes = listOf(Lane("c", pageCount = LAST + 1, state = acked, here = page(5), needsReview = true)), ops = listOf(unsent)))
            },
        ) {
            settle()
            assertEquals(emptyList<Any>(), writes())
            server.unreachable = true
            assertEquals(CommandOutcome.QUEUED, engine.command(Content("c", "Title c", ContentType.COMIC), op("mark_completed")))
            // The reading it replaces is gone, nothing is left to ask, and the server's state is read before it goes.
            val lane = store.lane("c")!!
            assertEquals(listOf(true, false, acked), listOf(lane.rebase, lane.needsReview, lane.state))
            val command = store.ops.single()
            assertEquals(listOf<Any?>(OpKind.COMMAND, null), listOf(command.kind, command.epoch))
            val end = endProgress(ContentType.COMIC, LAST + 1)
            assertEquals(Shown("completed", end, null, unsent = true, needsReview = false, projected = true), engine.shown(setOf("c")).first()["c"])

            val reader = attach("c")
            settle()
            assertEquals(page(5), reader.loading.getCompleted())
            reader.sync.detach()
            server.unreachable = false
            engine.drain()
            assertEquals("completed", server.stateOf("c").status)
            assertEquals(listOf("get", "mark_completed"), server.sent.takeLast(2).map { it.body?.string("op") ?: it.path })
            assertEquals(false, store.lane("c")?.rebase)
            // The answer's progress equals what was shown, in the server's key order: nothing is projected any more.
            assertEquals(false, engine.shown(setOf("c")).first()["c"]?.projected)
        }
    }

    @Test
    fun `a page with a later state brings the lane up to date, and an older one doesn't`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(5)) }
        engine.seed(listOf(fetched("c")))
        // Another device, then a page loaded after it: its command is made against what the page shows.
        server.set("c") { copy(status = "on_hold", progress = page(9)) }
        val fresh = fetched("c")
        assertEquals(CommandOutcome.APPLIED, engine.command(fresh, op("mark_completed")))
        assertEquals(fresh.userData?.revision, writes().single().string("base_revision"))
        assertEquals(0, store.ops.size)

        // A page whose state is older than the lane's changes nothing of it.
        val current = server.stateOf("c").revision
        assertEquals(CommandOutcome.APPLIED, engine.command(fresh, status("reading")))
        assertEquals(current, writes().last().string("base_revision"))
        assertEquals(2, writes().size)
    }

    @Test
    fun `queues what is made without the server, shows it at once, and sends it when the server is back`() = engineTest {
        server.series("s", "a", "b")
        server.set("a") { copy(status = "reading", progress = page(3)) }
        val a = fetched("a", parentId = "s")
        val series = fetched("s", ContentType.COMIC_SERIES)
        server.unreachable = true
        connectivity.online.value = false
        // A series command needs the series' volumes: the cached rows stand in for the server.
        val uncached = runCatching { engine.seriesCommand(series, SeriesAction.MARK_THROUGH, untilId = "b") }.exceptionOrNull()
        assertTrue(uncached is SyncUnavailable.NeedsConnection)
        assertEquals(emptyList<Any>(), store.ops)
        store.cache(listOf(series to null, a to 0, fetched("b", parentId = "s") to 1), wholeList = "s")

        catalog.begin(7)
        assertEquals(CommandOutcome.QUEUED, engine.command(a, status("on_hold"), origin = 7))
        assertEquals(CommandOutcome.QUEUED, engine.seriesCommand(series, SeriesAction.MARK_THROUGH, untilId = "b", origin = 7))
        val queued = signals.size
        assertEquals(listOf("a"), server.sent.map { it.id })
        assertEquals(listOf(0, null), store.ops.map { it.epoch })
        val shown = engine.shown(setOf("s", "a", "b", "x")).first()
        val end = JsonObject(mapOf("progress_percent" to JsonPrimitive(100), "at_end" to JsonPrimitive(true)))
        assertEquals(
            mapOf(
                "s" to Shown("reading", EmptyProgress, null, unsent = true, needsReview = false, projected = true),
                "a" to Shown("completed", end, null, unsent = true, needsReview = false, projected = true),
                "b" to Shown("completed", end, null, unsent = false, needsReview = false, projected = true),
            ),
            shown,
        )

        server.unreachable = false
        connectivity.online.value = true
        settle()
        // What drains while the batch is still open is ordinary, though the ops were made under it (U6): the engine detaches them, not the end.
        val drained = signals.drop(queued).filterIsInstance<Changed>()
        assertTrue(drained.isNotEmpty() && drained.all { it.batch == null })
        catalog.end(7, emptyList())
        assertEquals(listOf("completed", "completed", "reading"), listOf("a", "b", "s").map { server.stateOf(it).status })
        assertEquals(emptyList<Any>(), store.ops)
        // The answers replace what was shown: every lane shows what the server has, volumes included.
        val after = engine.shown(setOf("s", "a", "b")).first()
        for (id in listOf("s", "a", "b")) {
            val state = server.stateOf(id)
            assertEquals(Shown(state.status, state.progress, state.lastReadAt, unsent = false, needsReview = false, projected = false), after[id])
        }
    }

    @Test
    fun `shows a queued command until it is dropped, then what the lane adopted`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(5)) }
        val page = fetched("c")
        server.unreachable = true
        assertEquals(CommandOutcome.QUEUED, engine.command(page, op("clear")))
        assertEquals(Shown(null, EmptyProgress, null, unsent = true, needsReview = false, projected = true), engine.shown(setOf("c")).first()["c"])
        server.set("c") { copy(progress = page(9)) }
        server.unreachable = false
        engine.drain()
        assertEquals(Shown("reading", page(9), null, unsent = false, needsReview = false, projected = false), engine.shown(setOf("c")).first()["c"])
        assertEquals("clear", store.notices.single().detail.command)
    }
}
