package me.tijlvdb.voltis.data.reading

import kotlinx.coroutines.flow.first
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.domain.reading.AttentionItem
import me.tijlvdb.voltis.domain.reading.Change
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.reading.ConflictKind
import me.tijlvdb.voltis.domain.reading.DrainResult
import me.tijlvdb.voltis.domain.reading.Lane
import me.tijlvdb.voltis.domain.reading.Op
import me.tijlvdb.voltis.domain.reading.OpKind
import me.tijlvdb.voltis.domain.reading.PositionLabel
import me.tijlvdb.voltis.domain.reading.PromptChoice
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.ReadingState
import me.tijlvdb.voltis.domain.reading.ReviewOutcome
import me.tijlvdb.voltis.domain.reading.SeriesAction
import me.tijlvdb.voltis.domain.reading.SyncPrompt
import me.tijlvdb.voltis.domain.sync.NoticeKind
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** Not in the web: lanes changed elsewhere with nobody there to ask, and "Needs attention" (P2 §5, Held lanes). */
class EngineReviewTest : EngineTest() {
    /**
     * Stored before the engine starts: [id] read here to page [here] on top of page 2, unsent, and
     * read elsewhere since to [elsewhere] (with [status]). [held] lanes were already found changed.
     */
    private suspend fun readOffline(id: String, here: Int?, elsewhere: Int, status: String = "reading", held: Boolean = true, parentId: String? = null) {
        val acked = ReadingState("srv:$id-before", "reading", progress = page(2))
        val ops = listOfNotNull(here?.let { Op(contentId = id, kind = OpKind.POSITION, payload = page(it), sealed = true) })
        val lane = Lane(id, parentId = parentId, type = ContentType.COMIC, title = "Title $id", pageCount = LAST + 1, state = acked, here = here?.let(::page), needsReview = held)
        store.commit(Change(lanes = listOf(lane), ops = ops))
        server.set(id) { copy(status = status, progress = page(elsewhere)) }
    }

    private fun EngineTest.Harness.reviewing(id: String) = store.notices.isEmpty() && server.sent.none { it.id == id && it.body != null }

    private suspend fun EngineTest.Harness.needsReview(id: String) = engine.shown(setOf(id)).first()[id]?.needsReview

    @Test
    fun `with nobody there, the same position goes on top, a difference holds, and a held lane with nothing unsent adopts`() = engineTest(
        before = {
            // Only the status changed elsewhere; then a position moved; then a lane left held after a kill, with nothing unsent.
            readOffline("x", here = 5, elsewhere = 2, status = "on_hold", held = false)
            readOffline("y", here = 5, elsewhere = 8, held = false)
            readOffline("z", here = null, elsewhere = 8)
        },
    ) {
        assertEquals(DrainResult.DONE, engine.drain())
        assertEquals("reading" to page(5), server.stateOf("x").let { it.status to it.progress })
        assertEquals(page(8), server.stateOf("y").progress)
        assertEquals(page(8), store.lane("z")?.state?.progress)
        assertEquals(listOf(false, true, false), listOf("x", "y", "z").map { store.lane(it)?.needsReview })
        assertEquals(listOf(AttentionItem.Held("y", "Title y")), engine.held.first())
        // The held reading stays, unsent.
        assertEquals(listOf(page(5)), store.ops.map { it.payload })
        assertNull(prompt())
        // Without the server the held lane's check parks, but nothing of the outbox waits on it.
        server.unreachable = true
        assertEquals(DrainResult.DONE, engine.drain())
        // Signed out by a check after that: the sign-out waits.
        server.unreachable = false
        server.failWith = { ReadingFailure.SignedOut() }
        assertEquals(DrainResult.WAITING, engine.drain())
    }

    @Test
    fun `Review asks with both positions, and each answer does what the reader's would`() = engineTest(
        before = {
            readOffline("g", here = 5, elsewhere = 8)
            readOffline("s", here = 5, elsewhere = 8)
            readOffline("n", here = 5, elsewhere = 2, status = "on_hold")
        },
    ) {
        val go = review("g")
        assertEquals(SyncPrompt.Conflict(ConflictKind.MOVED, PositionLabel.Page(6), PositionLabel.Page(9)), go.prompt.value)
        go.answer(PromptChoice.GO)
        settle()
        assertEquals(page(8), store.lane("g")?.here)
        go.detach()

        val stay = review("s")
        stay.answer(PromptChoice.STAY)
        settle()
        stay.detach()
        settle()
        // Nothing to ask: Review resolves it as soon as it reads.
        review("n").detach()
        settle()
        assertNull(prompt())
        assertEquals(listOf(page(8), page(5), page(5)), listOf("g", "s", "n").map { server.stateOf(it).progress })
        assertEquals(listOf("s", "n"), server.sent.filter { it.body != null }.map { it.id })
        assertEquals(emptyList<AttentionItem.Held>(), engine.held.first())
        assertEquals(emptyList<Any>(), store.ops)
    }

    @Test
    fun `a prompt left unanswered keeps the lane held and frees the slot, whatever ends it`() = engineTest(
        before = { for (id in listOf("user", "background", "review", "reader", "stop")) readOffline(id, here = 5, elsewhere = 8) },
    ) {
        for (case in listOf("user", "background", "review", "reader", "stop")) {
            val session = if (case == "reader") attach(case).sync.also { settle() } else review(case)
            assertEquals(case, ConflictKind.MOVED, (prompt() as SyncPrompt.Conflict).kind)
            // Another item's status waits for the slot.
            val other = "other-$case"
            server.set(other) { copy(status = "reading") }
            var outcome: CommandOutcome? = null
            if (case != "stop") later { outcome = engine.command(fetched(other), status("on_hold")) }
            settle()
            assertNull(case, outcome)
            when (case) {
                "user" -> session.dismissPrompt()
                "background" -> engine.background()
                "review", "reader" -> session.detach()
                "stop" -> engine.stop()
            }
            settle()
            assertNull(case, session.prompt.value)
            // Not Stay: nothing of the lane is sent, and it still needs review.
            assertEquals(case, true, reviewing(case))
            assertEquals(case, true, store.lane(case)?.needsReview)
            if (case != "stop") assertEquals(case, CommandOutcome.APPLIED, outcome)
            if (case == "user" || case == "background") session.detach()
        }
    }

    @Test
    fun `a review ends itself once resolved, failed, or its dialog slot is free, and a Clear it confirmed is queued without a caller`() = engineTest(
        before = {
            readOffline("r", here = 5, elsewhere = 2, status = "on_hold")
            readOffline("f", here = 5, elsewhere = 8)
            readOffline("c", here = 5, elsewhere = 8, status = "completed")
        },
    ) {
        // Nothing to ask: resolved, and ended.
        val resolved = engine.review("r")
        settle()
        assertEquals(listOf(ReviewOutcome.RESOLVED, true), listOf(resolved.outcome.value, resolved.ended.value))
        assertEquals(false, needsReview("r"))
        // The read fails: ended, and the lane still held.
        server.failing = true
        val failed = engine.review("f")
        settle()
        server.failing = false
        assertEquals(listOf(ReviewOutcome.FAILED, true), listOf(failed.outcome.value, failed.ended.value))
        assertEquals(true, needsReview("f"))

        // Reset asks to confirm: the review lasts until that closes too.
        val asked = review("c")
        assertEquals(ReviewOutcome.ASKED, asked.outcome.value)
        asked.answer(PromptChoice.RESET)
        settle()
        assertEquals(listOf(SyncPrompt.ConfirmClear, false), listOf(asked.prompt.value, asked.ended.value))
        // Confirmed: nobody waits for the clear, so another device's write since drops it with a notice.
        server.holding = true
        asked.answer(PromptChoice.CONFIRM)
        settle()
        assertEquals(true, asked.ended.value)
        server.set("c") { copy(progress = page(9)) }
        server.release()
        settle()
        assertEquals(listOf("completed", page(9)), server.stateOf("c").let { listOf(it.status, it.progress) })
        val notice = store.notices.single()
        assertEquals(listOf("c", NoticeKind.CHANGED_ELSEWHERE, "clear", false), listOf(notice.contentId, notice.kind, notice.detail.command, notice.detail.uncertain))
        assertNull(prompt())

        // Parked, its read wouldn't go: it fails at once.
        server.unreachable = true
        engine.drain()
        val parked = engine.review("f")
        settle()
        assertEquals(listOf(ReviewOutcome.FAILED, true), listOf(parked.outcome.value, parked.ended.value))
    }

    @Test
    fun `a series op covering a held volume ends its review`() = engineTest(before = { readOffline("a", here = 5, elsewhere = 8, parentId = "s") }) {
        server.series("s", "a", "b")
        assertEquals(true, needsReview("a"))
        assertEquals(CommandOutcome.APPLIED, engine.seriesCommand(fetched("s", ContentType.COMIC_SERIES), SeriesAction.MARK_THROUGH, untilId = "b"))
        // The answer's volumes are read again and seeded.
        settle()
        val lane = store.lane("a")!!
        assertEquals(listOf(false, false, "completed"), listOf(lane.needsReview, lane.rebase, lane.state.status))
        assertEquals(listOf("completed", "completed"), listOf("a", "b").map { server.stateOf(it).status })
        // Its reading was replaced, never sent.
        assertEquals(listOf("series-reading"), server.sent.filter { it.body != null }.map { it.path })
        assertEquals(emptyList<AttentionItem.Held>(), engine.held.first())
    }

    @Test
    fun `a dismissed dialog isn't asked again on return, Review asks it, and drain is busy meanwhile`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        server.set("d") { copy(status = "reading", progress = page(2)) }
        val (sync, placements) = open("c")
        server.holding = true
        read(sync, 22)
        server.set("c") { copy(progress = page(79)) }
        server.release()
        settle()
        sync.moved(page(23))
        settle()
        assertEquals(ConflictKind.MOVED, (prompt() as SyncPrompt.Conflict).kind)
        sync.dismissPrompt()
        sync.setVisible(false)
        sync.setVisible(true)
        settle()
        assertNull(prompt())
        assertEquals(true, sync.view.value.stale)
        // Another volume still saves.
        val (other) = open("d")
        read(other, 4)
        assertEquals(page(4), server.stateOf("d").progress)
        sync.check()
        settle()
        assertEquals(ConflictKind.MOVED, (prompt() as SyncPrompt.Conflict).kind)
        assertEquals(DrainResult.BUSY, engine.drain())
        answer(PromptChoice.STAY)
        advance(1000)
        assertEquals(page(23), server.stateOf("c").progress)

        // Reviewed from elsewhere while the reader is open: Go there moves the reader once the lane is back.
        server.set("c") { copy(progress = page(40)) }
        sync.moved(page(24))
        settle()
        sync.dismissPrompt()
        settle()
        val reviewer = review("c")
        assertEquals(ConflictKind.MOVED, (reviewer.prompt.value as SyncPrompt.Conflict).kind)
        reviewer.answer(PromptChoice.GO)
        settle()
        assertEquals(true, reviewer.ended.value)
        assertEquals(page(40), placements.last())
        read(sync, 41)
        assertEquals(page(41), server.stateOf("c").progress)
    }
}
