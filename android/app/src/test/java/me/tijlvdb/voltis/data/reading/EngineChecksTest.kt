package me.tijlvdb.voltis.data.reading

import kotlinx.coroutines.async
import kotlinx.coroutines.flow.first
import kotlinx.serialization.json.JsonObject
import me.tijlvdb.voltis.domain.reading.AttentionItem
import me.tijlvdb.voltis.domain.reading.ConflictKind
import me.tijlvdb.voltis.domain.reading.DrainResult
import me.tijlvdb.voltis.domain.reading.EmptyProgress
import me.tijlvdb.voltis.domain.reading.Change
import me.tijlvdb.voltis.domain.reading.Lane
import me.tijlvdb.voltis.domain.reading.Op
import me.tijlvdb.voltis.domain.reading.OpKind
import me.tijlvdb.voltis.domain.reading.PositionLabel
import me.tijlvdb.voltis.domain.reading.PromptChoice
import me.tijlvdb.voltis.domain.reading.ReadingSession
import me.tijlvdb.voltis.domain.reading.SyncAction
import me.tijlvdb.voltis.domain.reading.SyncNotice
import me.tijlvdb.voltis.domain.reading.SyncPrompt
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** readingSync.test.ts, `checks`. The web's `sync.check()` on a page shown again is `setVisible(true)`. */
class EngineChecksTest : EngineTest() {
    @Test
    fun `reads before unsent reading goes, and collapses repeats`() = engineTest {
        val (sync, placements) = open("c")
        sync.moved(page(4))
        sync.check()
        sync.check()
        advance(1000)
        assertEquals(listOf("get", "get", "position"), server.sent.map { it.body?.string("op") ?: it.path })
        assertEquals(emptyList<Any>(), placements)
        assertNull(prompt())
    }

    @Test
    fun `follows another device when idle, with an Undo that writes nothing`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync, placements) = open("c")
        server.set("c") { copy(progress = page(53)) }
        sync.check()
        settle()
        assertEquals(listOf(page(53)), placements)
        val followed = notice<SyncNotice.Followed>()
        assertEquals(PositionLabel.Page(54), (followed.notice as SyncNotice.Followed).label)
        followed.action!!()
        settle()
        assertEquals(listOf(page(53), page(2)), placements)
        advance(5000)
        assertEquals(emptyList<Any>(), writes())
    }

    /** With a position, and without one. */
    @Test
    fun `informs of a status cleared on its own elsewhere`() = eachCase(listOf(page(2), EmptyProgress)) { clearedOnItsOwn(it) }

    private fun clearedOnItsOwn(progress: JsonObject) = engineTest {
        server.set("c") { copy(status = "reading", progress = progress, statusUpdatedAt = "then") }
        val (sync) = open("c")
        server.set("c") { copy(status = null, statusUpdatedAt = "now") }
        sync.check()
        settle()
        assertNull(prompt())
        assertNull(sync.view.value.acked?.status)
        assertEquals(SyncNotice.StatusElsewhere(null), notice<SyncNotice.StatusElsewhere>().notice)
    }

    @Test
    fun `informs of a status changed elsewhere`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync) = open("c")
        server.set("c") { copy(status = "on_hold") }
        sync.check()
        settle()
        assertEquals("on_hold", sync.view.value.acked?.status)
        assertEquals(SyncNotice.StatusElsewhere("on_hold"), notice<SyncNotice.StatusElsewhere>().notice)
    }

    @Test
    fun `asks when both moved`() = eachCase(listOf(PromptChoice.STAY to 23, PromptChoice.GO to 79)) { (choice, saved) -> bothMoved(choice, saved) }

    private fun bothMoved(choice: PromptChoice, saved: Int) = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync, placements) = open("c")
        server.holding = true
        read(sync, 22)
        server.set("c") { copy(progress = page(79)) }
        server.release()
        settle()
        sync.moved(page(23))
        assertEquals(SyncPrompt.Conflict(ConflictKind.MOVED, PositionLabel.Page(23), PositionLabel.Page(80)), prompt())
        answer(choice)
        advance(1000)
        assertEquals(page(saved), server.stateOf("c").progress)
        assertEquals(if (choice == PromptChoice.GO) listOf(page(79)) else emptyList(), placements)
    }

    /** A conflict about a moved item, dismissed: the lane stays held, with its reading retained. */
    private suspend fun Harness.dismissedConflict(): ReadingSession {
        server.series("s", "c")
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync) = open("c")
        server.holding = true
        read(sync, 22)
        server.set("c") { copy(progress = page(79)) }
        server.release()
        settle()
        sync.moved(page(23))
        assertEquals(ConflictKind.MOVED, (prompt() as SyncPrompt.Conflict).kind)
        sync.dismissPrompt()
        settle()
        return sync
    }

    @Test
    fun `keeps the outbox to one position while a conflict is dismissed`() = engineTest {
        val sync = dismissedConflict()
        advance(1000)
        val before = store.ops.count { it.kind == OpKind.POSITION }
        for (n in 30..39) read(sync, n)
        assertEquals(before, store.ops.count { it.kind == OpKind.POSITION })
        assertEquals(page(39), store.ops.last().payload)
    }

    @Test
    fun `a failed rebase read is tried again once a read of the lane succeeds, reader there`() {
        server.failing = true
        engineTest(
            before = {
                val command = Op(contentId = "c", kind = OpKind.COMMAND, payload = status("on_hold"), after = listOf("c"))
                store.commit(Change(lanes = listOf(Lane("c", rebase = true)), ops = listOf(command)))
            },
        ) {
            // The read the command waits for failed: the worker is asked, and nothing is sent.
            settle()
            assertEquals(listOf("get"), server.sent.map { it.path })
            assertTrue(scheduler.scheduled > 0)
            server.failing = false
            // The reader's own read of the lane answers, and the command's rebase goes ahead.
            open("c")
            advance(1000)
            assertEquals(listOf("set_status"), ops())
        }
    }

    @Test
    fun `a conflict answer whose series lookup fails still applies the choice`() =
        eachCase(listOf(PromptChoice.STAY to 23, PromptChoice.GO to 79)) { (choice, saved) -> failingSeriesLookup(choice, saved) }

    /** STAY sends the retained reading; GO drops it, and a reopen doesn't replay it over the remote position. */
    private fun failingSeriesLookup(choice: PromptChoice, saved: Int) = engineTest {
        server.series("s", "c")
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync) = open("c")
        server.holding = true
        read(sync, 22)
        // The answer comes with a series the engine has never looked up: its lane is read from the store, which refuses.
        server.series("t", "c")
        server.set("c") { copy(progress = page(79)) }
        server.release()
        settle()
        sync.moved(page(23))
        assertEquals(ConflictKind.MOVED, (prompt() as SyncPrompt.Conflict).kind)
        var fired = 0
        faults.failLane = { (it == "t").also { hit -> if (hit) fired++ } }
        answer(choice)
        assertTrue(fired > 0)
        // The choice was applied: no dialog, nothing held, and the slot is free.
        assertNull(prompt())
        assertEquals(emptyList<AttentionItem.Held>(), engine.held.first())
        assertNotEquals(DrainResult.BUSY, engine.drain())
        sync.detach()
        settle()
        open("c")
        advance(1000)
        assertEquals(page(saved), server.stateOf("c").progress)
        assertEquals(emptyList<AttentionItem.Held>(), engine.held.first())
    }

    /** By a drain alone, and by one after a plain check. */
    @Test
    fun `a failed rebase read with only reading queued is finished by a drain`() = eachCase(listOf(false, true)) { rebaseOfReading(check = it) }

    private fun rebaseOfReading(check: Boolean) {
        server.holding = true
        engineTest(
            before = {
                val position = Op(contentId = "c", kind = OpKind.POSITION, payload = page(5), sealed = true)
                store.commit(Change(lanes = listOf(Lane("c", rebase = true)), ops = listOf(position)))
            },
        ) {
            // The reader is there when the rebase read, already out, fails with a 500.
            val reader = attach("c")
            settle()
            server.failing = true
            server.release()
            settle()
            assertEquals(emptyList<String?>(), ops())
            server.failing = false
            if (check) {
                reader.sync.check()
                settle()
            }
            engine.drain()
            advance(1000)
            assertEquals(listOf("position"), ops())
        }
    }

    @Test
    fun `a dismissed dialog isn't asked again by a check queued before it`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync) = open("c")
        server.holding = true
        read(sync, 22)
        sync.check()
        server.set("c") { copy(progress = page(79)) }
        server.release()
        settle()
        assertEquals(ConflictKind.MOVED, (prompt() as SyncPrompt.Conflict).kind)
        sync.dismissPrompt()
        settle()
        assertNull(prompt())
    }

    @Test
    fun `drops reading whose send failed when the reader goes to the other device`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync) = open("c")
        server.holding = true
        read(sync, 3)
        sync.check()
        server.failing = true
        server.release()
        settle()
        server.failing = false
        server.set("c") { copy(progress = page(79)) }
        sync.check()
        settle()
        answer(PromptChoice.GO)
        sync.retry()
        advance(5000)
        assertEquals(page(79), server.stateOf("c").progress)
    }

    @Test
    fun `holds a conflict that came in while hidden until the page is back`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync) = open("c")
        server.set("c") { copy(progress = page(79)) }
        sync.moved(page(5))
        sync.setVisible(false)
        settle()
        assertEquals(true, sync.view.value.stale)
        assertNull(prompt())
        sync.setVisible(true)
        settle()
        answer(PromptChoice.STAY)
        settle()
        assertEquals(false, sync.view.value.stale)
        assertEquals(page(5), server.stateOf("c").progress)
        assertNull(prompt())
    }

    @Test
    fun `opens one dialog at a time across a held series, a conflict and focus`() = engineTest {
        server.series("s", "c")
        server.set("s") { copy(status = "dropped") }
        val (sync) = open("c")
        read(sync, 1)
        assertEquals(SyncPrompt.SeriesHeld("dropped", null), prompt())
        server.set("c") { copy(progress = page(60)) }
        read(sync, 2)
        sync.setVisible(true)
        settle()
        assertEquals(SyncPrompt.SeriesHeld("dropped", null), prompt())
        answer(PromptChoice.KEEP)
        settle()
        assertEquals(ConflictKind.MOVED, (prompt() as SyncPrompt.Conflict).kind)
        answer(PromptChoice.STAY)
        settle()
        assertNull(prompt())
        assertEquals(page(2), server.stateOf("c").progress)
        assertEquals("dropped", server.stateOf("s").status)
    }

    @Test
    fun `checks only once the open dialog is answered`() = engineTest {
        server.series("s", "c")
        server.set("s") { copy(status = "on_hold") }
        val (sync, placements) = open("c")
        read(sync, 1)
        server.clear("c")
        sync.check()
        settle()
        assertEquals(SyncPrompt.SeriesHeld("on_hold", null), prompt())
        answer(PromptChoice.KEEP)
        assertEquals(ConflictKind.RESET, (prompt() as SyncPrompt.Conflict).kind)
        answer(PromptChoice.START)
        assertEquals(listOf(EmptyProgress), placements)
    }

    @Test
    fun `compares an item changed elsewhere when a series command returns`() = engineTest {
        server.series("s", "c")
        server.set("s") { copy(status = "on_hold") }
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync, placements) = open("c")
        server.set("c") { copy(progress = page(79)) }
        sync.seriesCommand("reading")
        settle()
        assertEquals(listOf(page(79)), placements)
        sync.moved(page(3))
        server.set("c") { copy(progress = page(60)) }
        val command = testScope.async { sync.seriesCommand("reading") }
        settle()
        assertEquals(PositionLabel.Page(61), (prompt() as SyncPrompt.Conflict).saved)
        answer(PromptChoice.GO)
        command.await()
        advance(1000)
        assertEquals(page(60), server.stateOf("c").progress)
    }

    @Test
    fun `keeps a clear acknowledged after a check that read the state before it`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(3)) }
        val (sync, placements) = open("c")
        sync.check()
        sync.resetAndReadAgain()
        settle()
        answer(PromptChoice.CONFIRM)
        assertNull(server.stateOf("c").status)
        assertEquals(listOf(EmptyProgress), placements)
    }

    /** The web's rejection: task phase 8 makes it the drop rule, with the same writes and the same danger message. */
    @Test
    fun `asks again after a clear that conflicted, against the new state`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(3)) }
        val (sync, placements) = open("c")
        sync.resetAndReadAgain()
        settle()
        server.set("c") { copy(status = "on_hold") }
        answer(PromptChoice.CONFIRM)
        assertEquals(SyncAction.CLEAR, (notice<SyncNotice.Failed>().notice as SyncNotice.Failed).what)
        assertEquals("on_hold", sync.view.value.acked?.status)
        sync.resetAndReadAgain()
        settle()
        answer(PromptChoice.CONFIRM)
        assertNull(server.stateOf("c").status)
        assertEquals(listOf(EmptyProgress), placements)
        assertEquals(listOf("clear", "clear"), ops())
    }

    @Test
    fun `keeps reading a conflict met after the reader left, and asks on reopening`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val first = open("c")
        server.holding = true
        later { first.sync.command(op("set_status", "status" to "reading")) }
        settle()
        first.sync.finish(END)
        first.sync.detach()
        server.set("c") { copy(progress = page(40)) }
        server.release()
        settle()
        assertEquals("reading", server.stateOf("c").status)
        open("d")
        val again = attach("c")
        settle()
        assertEquals(SyncPrompt.Conflict(ConflictKind.MOVED, PositionLabel.Page(10), PositionLabel.Page(41)), prompt())
        answer(PromptChoice.STAY)
        again.loading.await()
        assertEquals("completed", server.stateOf("c").status)
    }
}
