package me.tijlvdb.voltis.data.reading

import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.SeriesAction
import me.tijlvdb.voltis.domain.reading.SyncAction
import me.tijlvdb.voltis.domain.reading.SyncNotice
import me.tijlvdb.voltis.domain.reading.Undoable
import me.tijlvdb.voltis.domain.reading.guardVolumes
import me.tijlvdb.voltis.domain.sync.NoticeKind
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Not in the web: commands judged against what they were made against (P2 §5, Guards). */
class EngineGuardTest : EngineTest() {
    /** Without the server: commands queue, and series commands take their volumes from the cached rows. */
    private fun EngineTest.Harness.offline(cut: Boolean) {
        server.unreachable = cut
        connectivity.online.value = !cut
        settle()
    }

    /** [seriesId] and its volumes cached as a page fetched them, in the server's order. */
    private suspend fun EngineTest.Harness.cache(seriesId: String) {
        val volumes = server.parents.filterValues { it == seriesId }.keys.filter { it !in server.invalid }
        store.cache(listOf(fetched(seriesId, ContentType.COMIC_SERIES) to null) + volumes.mapIndexed { i, id -> fetched(id) to i }, wholeList = seriesId)
    }

    private fun EngineTest.Harness.seriesWrites(seriesId: String) = server.sent.filter { it.id == seriesId && it.body != null }

    @Test
    fun `clear and mark completed are dropped when the item changed elsewhere, a plain status is not`() = engineTest {
        for (id in listOf("c", "d", "e", "m")) server.set(id) { copy(status = "reading", progress = page(5)) }
        val (c, d, e) = listOf("c", "d", "e").map { fetched(it) }
        // Queued, then sent over another device's write: the 409 drops the clear and a status of completed, and the status goes again on what it adopted.
        offline(true)
        assertEquals(CommandOutcome.QUEUED, engine.command(c, op("clear")))
        assertEquals(CommandOutcome.QUEUED, engine.command(e, status("on_hold")))
        assertEquals(CommandOutcome.QUEUED, engine.command(fetched("m"), status("completed")))
        for (id in listOf("c", "e", "m")) server.set(id) { copy(progress = page(9)) }
        offline(false)
        engine.drain()
        assertEquals("reading" to page(9), server.stateOf("c").let { it.status to it.progress })
        assertEquals("on_hold" to page(9), server.stateOf("e").let { it.status to it.progress })
        assertEquals("reading", server.stateOf("m").status)
        assertEquals(
            listOf(listOf("c", NoticeKind.CHANGED_ELSEWHERE, "clear"), listOf("m", NoticeKind.CHANGED_ELSEWHERE, "mark_completed")),
            store.notices.map { listOf(it.contentId, it.kind, it.detail.command) },
        )

        // Behind a status whose 409 adopted another device's state: its epoch moved, so it is dropped
        // unsent, and its caller is told, with nothing stored.
        server.holding = true
        later { engine.command(d, status("on_hold")) }
        var dropped: Throwable? = null
        later { dropped = runCatching { engine.command(d, op("mark_completed")) }.exceptionOrNull() }
        settle()
        server.set("d") { copy(progress = page(9)) }
        server.release()
        settle()
        assertEquals("mark_completed", (dropped as ReadingFailure.ChangedElsewhere).command)
        assertEquals("on_hold" to page(9), server.stateOf("d").let { it.status to it.progress })
        assertTrue(writes().none { it.string("op") == "mark_completed" })
        assertEquals(2, store.notices.size)
        assertEquals(emptyList<Any>(), store.ops)

        // A status meeting another device's write while its lane has unsent reading, with the reader
        // hidden: it waits with the held lane, and its caller hears so.
        server.set("p") { copy(status = "reading", progress = page(2)) }
        val (p) = open("p")
        server.holding = true
        var outcome: CommandOutcome? = null
        later { outcome = engine.command(fetched("p"), status("on_hold")) }
        settle()
        p.moved(page(3))
        p.setVisible(false)
        settle()
        server.set("p") { copy(progress = page(7)) }
        server.release()
        settle()
        assertEquals(CommandOutcome.QUEUED, outcome)
        assertEquals(true, store.lane("p")?.needsReview)
        assertEquals(listOf("command", "position"), store.ops.map { it.kind })
        // Under the banner a replacing command is the user's choice: it carries no epoch to drop it by.
        later { p.command(op("mark_completed")) }
        settle()
        assertNull(store.ops.single { it.payload.string("op") == "mark_completed" }.epoch)
    }

    @Test
    fun `series commands are dropped when a covered volume moved elsewhere, and go when only our writes touched it`() = engineTest {
        val actions = listOf(SeriesAction.MARK_THROUGH, SeriesAction.MARK_SERIES_COMPLETED, SeriesAction.CLEAR)
        for ((n, action) in actions.withIndex()) {
            val s = "s$n"
            val (a, b) = listOf("${s}a", "${s}b")
            server.series(s, a, b)
            server.set(a) { copy(status = "reading", progress = page(3)) }
            suspend fun make() = when (action) {
                SeriesAction.CLEAR -> engine.command(fetched(s, ContentType.COMIC_SERIES), op("clear"))
                else -> engine.seriesCommand(fetched(s, ContentType.COMIC_SERIES), action, includeUnread = true, untilId = b)
            }
            cache(s)
            offline(true)
            assertEquals(CommandOutcome.QUEUED, make())
            // Another device reads a covered volume: nothing is written, and the notice says why.
            server.set(b) { copy(status = "reading", progress = page(4)) }
            offline(false)
            engine.drain()
            assertEquals(action, emptyList<Any>(), seriesWrites(s))
            assertEquals(action, "reading" to page(4), server.stateOf(b).let { it.status to it.progress })
            assertEquals(action, listOf(s, action), store.notices.last().let { listOf(it.contentId, it.detail.command) })

            // Made again, with a status of ours on a covered volume landing before it: it goes.
            offline(true)
            assertEquals(CommandOutcome.QUEUED, engine.command(fetched(a), status("on_hold")))
            assertEquals(CommandOutcome.QUEUED, make())
            offline(false)
            engine.drain()
            val expected = if (action == SeriesAction.CLEAR) null else "completed"
            assertEquals(action, listOf(expected, expected), listOf(a, b).map { server.stateOf(it).status })
            assertEquals(action, n + 1, store.notices.size)
            assertEquals(emptyList<Any>(), store.ops)
        }
    }

    @Test
    fun `the guard follows the list's order, and a volume covered at send but not in the guard counts as untouched then`() = engineTest {
        server.series("t", "t3", "t1", "t2")
        server.holding = true
        server.holds = { it.path == "series-reading" }
        later { engine.seriesCommand(fetched("t", ContentType.COMIC_SERIES), SeriesAction.MARK_THROUGH, untilId = "t1") }
        settle()
        assertEquals(listOf("t3", "t1"), store.ops.single().guard!!.guardVolumes().toList())
        server.release()
        settle()
        assertEquals(listOf("completed", "completed", null), listOf("t3", "t1", "t2").map { server.stateOf(it).status })

        // A volume the list left out when the command was made, listed at send: read elsewhere it drops
        // the command, untouched it is completed with the rest.
        for ((s, touched) in listOf("u" to true, "v" to false)) {
            server.series(s, "${s}1", "${s}2")
            server.invalid += "${s}2"
            cache(s)
            offline(true)
            assertEquals(CommandOutcome.QUEUED, engine.seriesCommand(fetched(s, ContentType.COMIC_SERIES), SeriesAction.MARK_SERIES_COMPLETED, includeUnread = true))
            assertEquals(listOf("${s}1"), store.ops.single().guard!!.guardVolumes().toList())
            server.invalid -= "${s}2"
            if (touched) server.set("${s}2") { copy(status = "reading", progress = page(1)) }
            offline(false)
            engine.drain()
            val status = if (touched) null else "completed"
            assertEquals(s, listOf(status, if (touched) "reading" else "completed"), listOf("${s}1", "${s}2").map { server.stateOf(it).status })
        }
        assertEquals(listOf("u"), store.notices.map { it.contentId })
    }

    @Test
    fun `a mark_through whose volume is gone is dropped alone`() = engineTest {
        server.series("w", "w1", "w2")
        cache("w")
        offline(true)
        val series = fetched("w", ContentType.COMIC_SERIES)
        assertEquals(CommandOutcome.QUEUED, engine.seriesCommand(series, SeriesAction.MARK_THROUGH, untilId = "w2"))
        assertEquals(CommandOutcome.QUEUED, engine.command(series, status("on_hold")))
        server.invalid += "w2"
        offline(false)
        engine.drain()
        // The 404 is the volume's: the series' own status still goes.
        assertNull(server.stateOf("w1").status)
        assertEquals("on_hold", server.stateOf("w").status)
        // Not "No longer on the server": the series is there.
        assertEquals(listOf("w", NoticeKind.REFUSED, "Volume not found"), store.notices.single().let { listOf(it.contentId, it.kind, it.detail.message) })
        assertEquals(emptyList<Any>(), store.ops)
        assertEquals(emptyList<String>(), gone)
    }

    @Test
    fun `an Undo is dropped when the item changed elsewhere, and tracking stays off`() = engineTest {
        server.set("c") { copy(status = "dropped", progress = page(1)) }
        val (sync) = open("c")
        read(sync, 2)
        val offer = offer(Undoable.MOVED_TO_READING)
        server.holding = true
        offer.action!!()
        settle()
        server.set("c") { copy(progress = page(7)) }
        server.release()
        settle()
        val failed = failure(SyncAction.UNDO)
        assertTrue((failed.notice as SyncNotice.Failed).error is ReadingFailure.ChangedElsewhere)
        // Retrying would restore over the other device's write.
        assertNull(failed.action)
        assertEquals("reading" to page(7), server.stateOf("c").let { it.status to it.progress })
        assertEquals(false, sync.view.value.tracking)
        read(sync, 8)
        assertEquals(listOf("position", "restore"), ops())
        assertEquals(emptyList<Any>(), store.notices)

        // Not yet sent when a read adopts another device's state: dropped unsent.
        server.set("u") { copy(status = "dropped", progress = page(1)) }
        val (u) = open("u")
        read(u, 2)
        val undo = offer(Undoable.MOVED_TO_READING)
        // A check without the server parks, and marks the lane to be read first once it answers.
        offline(true)
        u.check()
        settle()
        undo.action!!()
        settle()
        server.set("u") { copy(progress = page(7)) }
        offline(false)
        val undoFailures = notices.filter { (it.notice as? SyncNotice.Failed)?.what == SyncAction.UNDO }
        assertEquals(2, undoFailures.size)
        assertTrue((undoFailures.last().notice as SyncNotice.Failed).error is ReadingFailure.ChangedElsewhere)
        assertEquals(1, ops().count { it == "restore" })
        assertEquals("reading" to page(7), server.stateOf("u").let { it.status to it.progress })
    }
}
