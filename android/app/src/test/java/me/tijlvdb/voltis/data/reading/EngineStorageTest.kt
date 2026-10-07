package me.tijlvdb.voltis.data.reading

import kotlin.coroutines.CoroutineContext
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.content.CatalogChange.ReadingChanged
import me.tijlvdb.voltis.domain.reading.Change
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.reading.ConflictKind
import me.tijlvdb.voltis.domain.reading.DrainResult
import me.tijlvdb.voltis.domain.reading.Lane
import me.tijlvdb.voltis.domain.reading.Op
import me.tijlvdb.voltis.domain.reading.OpKind
import me.tijlvdb.voltis.domain.reading.ReadingState
import me.tijlvdb.voltis.domain.reading.SeriesAction
import me.tijlvdb.voltis.domain.reading.SyncAction
import me.tijlvdb.voltis.domain.reading.Undoable
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.domain.reading.PromptChoice
import me.tijlvdb.voltis.domain.reading.SyncNotice
import me.tijlvdb.voltis.domain.reading.SyncPrompt
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.junit.runners.Parameterized

/**
 * Not in the web: what reaches the user waits for the commit it belongs to, unless it announces nothing
 * stored; a store that refuses keeps the whole batch (P5 §5).
 */
class EngineStorageTest : EngineTest() {
    /** A commit that is held, and one that fails until one succeeds. */
    @Test
    fun `nothing escapes before the commit it belongs to`() = eachCase(listOf(true, false)) { effectsWaitForTheirCommit(hold = it) }

    private fun effectsWaitForTheirCommit(hold: Boolean) = engineTest {
        for (id in listOf("c1", "c2", "c3")) server.set(id) { copy(status = "reading", progress = page(2)) }
        val (c1) = open("c1")
        fun view() = c1.view.value.let { it.acked?.status to it.unsent }
        val c2 = open("c2")
        val (c3) = open("c3")
        fun fault(match: (Change) -> Boolean) = if (hold) faults.hold = match else faults.fail = match
        fun recover() {
            if (hold) {
                faults.release()
                settle()
            } else {
                faults.fail = null
                advance(ReadingEngine.STORAGE_RETRY)
            }
        }

        // A command's answer and its cache event.
        fault { it.deleteOps.isNotEmpty() }
        val result = CompletableDeferred<CommandOutcome>()
        later { result.complete(c1.command(op("set_status", "status" to "on_hold"))) }
        settle()
        assertEquals("on_hold", server.stateOf("c1").status)
        assertFalse(result.isCompleted)
        // The answer's state is stored with its op's retiring: before that commit, neither is.
        assertEquals(listOf(true, false), listOf(store.ops.isNotEmpty(), store.snapshot("c1")?.seq == server.stateOf("c1").seq))
        assertEquals(emptyList<Any>(), changes)
        // Held: the view shows what is stored, the command still unsent. Refused: it follows memory, where the server's answer is adopted.
        assertEquals(if (hold) "reading" to true else "on_hold" to false, view())
        recover()
        assertEquals(CommandOutcome.APPLIED, result.getCompleted())
        assertEquals(listOf(emptyList<Any>(), server.stateOf("c1").seq), listOf(store.ops, store.snapshot("c1")?.seq))
        assertEquals(listOf(ReadingChanged("c1", "c1")), changes)
        assertEquals("on_hold" to false, view())

        // Following another device: the placement and its snackbar.
        server.set("c2") { copy(progress = page(53)) }
        fault { change -> change.lanes.any { it.contentId == "c2" } }
        c2.sync.check()
        settle()
        // A refused lane row doesn't hold a placement back: it announces nothing stored.
        assertEquals(if (hold) emptyList<Any>() else listOf(page(53)), c2.placements)
        assertEquals(!hold, notices.any { it.notice is SyncNotice.Followed })
        recover()
        assertEquals(listOf(page(53)), c2.placements)
        notice<SyncNotice.Followed>()

        // A page turn made while the placement waits wins over it.
        server.set("c2") { copy(progress = page(60)) }
        fault { change -> change.lanes.any { it.contentId == "c2" } }
        c2.sync.check()
        settle()
        c2.sync.moved(page(54))
        settle()
        recover()
        assertEquals(if (hold) listOf(page(53)) else listOf(page(53), page(60)), c2.placements)
        assertEquals(page(54), store.lane("c2")?.here)
        advance(1000)
        assertEquals(page(54), server.stateOf("c2").progress)

        // A conflict's prompt.
        c3.moved(page(5))
        settle()
        server.set("c3") { copy(progress = page(79)) }
        fault { change -> change.lanes.any { it.contentId == "c3" && it.needsReview } }
        c3.check()
        settle()
        // Refused, the prompt still opens: it claims nothing stored, and the choice is asked again after a kill.
        assertEquals(if (hold) null else ConflictKind.MOVED, (prompt() as? SyncPrompt.Conflict)?.kind)
        recover()
        assertEquals(ConflictKind.MOVED, (prompt() as SyncPrompt.Conflict).kind)
        answer(PromptChoice.STAY)
        advance(1000)
        assertEquals(page(5), server.stateOf("c3").progress)
    }

    @Test
    fun `the reading hold lasts until what the engine holds is stored`() = engineTest {
        val (c) = open("c1")
        settle()
        assertFalse(ReadingHolds.held(ACCOUNT_DIR))
        val released = ReadingHolds.released(ACCOUNT_DIR)
        val before = released.value

        // A page turn that the store refuses: the batch is kept in memory, and the hold with it, event after event.
        faults.fail = { true }
        c.moved(page(5))
        settle()
        assertTrue(ReadingHolds.held(ACCOUNT_DIR))
        c.moved(page(6))
        settle()
        assertTrue(ReadingHolds.held(ACCOUNT_DIR))
        assertEquals(before, released.value)
        // The next iteration without a refused batch clears it, and says so.
        faults.fail = null
        advance(ReadingEngine.STORAGE_RETRY)
        assertFalse(ReadingHolds.held(ACCOUNT_DIR))
        assertTrue(released.value > before)

        // Stopping clears it too, with the batch still refused.
        faults.fail = { true }
        c.moved(page(7))
        settle()
        assertTrue(ReadingHolds.held(ACCOUNT_DIR))
        val beforeStop = released.value
        engine.stop()
        assertFalse(ReadingHolds.held(ACCOUNT_DIR))
        assertTrue(released.value > beforeStop)

        // So does an engine whose loop ends with its parent.
        faults.fail = null
        val parent = Job()
        newEngine(CoroutineScope(parent + StandardTestDispatcher(testScope.testScheduler))).start()
        settle()
        ReadingHolds.set(ACCOUNT_DIR, true)
        val beforeShutDown = released.value
        parent.cancel()
        settle()
        assertFalse(ReadingHolds.held(ACCOUNT_DIR))
        assertTrue(released.value > beforeShutDown)
    }

    @Test
    fun `a caller that leaves while its enqueue is held leaves an op that a server error keeps`() = engineTest {
        val (sync) = open("c")
        faults.hold = { change -> change.ops.any { it.id == 0L } }
        val caller = testScope.backgroundScope.launch { sync.command(op("set_status", "status" to "on_hold")) }
        settle()
        caller.cancel()
        server.failing = true
        faults.release()
        settle()
        advance(60_000)
        // Nobody watches it now: kept, tried once, and not again before a drain.
        assertEquals(listOf("set_status"), ops())
        assertEquals(1, store.ops.size)
        server.failing = false
        assertEquals(DrainResult.DONE, engine.drain())
        assertEquals("on_hold", server.stateOf("c").status)
    }

    @Test
    fun `a caller cancelled while its dispatcher is busy leaves an op that a server error keeps`() = engineTest {
        val (sync) = open("c")
        val gate = Gate()
        server.holding = true
        val caller = testScope.backgroundScope.launch(gate) { sync.command(op("set_status", "status" to "on_hold")) }
        gate.run()
        settle()
        assertEquals(1, server.inFlight)
        // Cancelled, but not yet resumed: its dispatcher hasn't run when the 500 comes in.
        caller.cancel()
        server.failing = true
        server.release()
        settle()
        assertEquals(1, store.ops.size)
        server.failing = false
        assertEquals(DrainResult.DONE, engine.drain())
        assertEquals("on_hold", server.stateOf("c").status)
        gate.run()
    }

    /** A full disk stops writes, not reading: loads answer, and the position waits in memory until a commit goes through. */
    @Test
    fun `a full disk keeps reading, holds the position in memory, and sends it once a commit goes through`() = engineTest {
        server.set("c1") { copy(status = "reading", progress = page(2)) }
        server.set("c2") { copy(status = "reading", progress = page(3)) }
        val (c1) = open("c1")
        faults.fail = { true }
        read(c1, 5)
        assertEquals(emptyList<Any>(), writes())
        assertTrue(c1.view.value.storageFull)
        // Another volume opens without waiting for space.
        assertEquals(page(3), open("c2").opened)
        // Turning pages once space is back stores, un-parks and sends, with no foreground and no Retry.
        faults.fail = null
        read(c1, 6)
        assertFalse(c1.view.value.storageFull)
        assertEquals(page(6), server.stateOf("c1").progress)
        assertEquals(emptyList<Any>(), store.ops)
    }

    /** A page turn merged into a stored, unsent position isn't stored either; nor is it said to be "saved on this device". */
    @Test
    fun `a position merged into a stored one while storage is full and the server unreachable shows as unsaved`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (c) = open("c")
        server.unreachable = true
        // The first goes out and finds no server; the second is stored, and never marked, as the pump is parked.
        read(c, 3)
        read(c, 4)
        assertEquals(listOf(page(3), page(4)), store.ops.map { it.payload })
        assertFalse(c.view.value.storageFull)
        assertTrue(c.view.value.parked)
        faults.fail = { true }
        read(c, 5)
        assertTrue(c.view.value.storageFull)
        // Killed now, the stored position is the earlier one.
        assertEquals(listOf(page(3), page(4)), store.ops.map { it.payload })
        faults.fail = null
        advance(ReadingEngine.STORAGE_RETRY)
        assertFalse(c.view.value.storageFull)
        assertEquals(listOf(page(3), page(5)), store.ops.map { it.payload })
    }

    /** The writer's identity file refuses while Room takes writes: parked, one attempt per retry, reads open, and it goes once the file takes writes. */
    @Test
    fun `a refused writer reservation stays parked with one attempt per retry until space returns`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        server.set("d") { copy(status = "reading", progress = page(3)) }
        val (c) = open("c")
        writer.full = true
        read(c, 3)
        assertEquals(1, writer.reservations)
        assertEquals(emptyList<Any>(), writes())
        // Room-only and empty commits don't clear it; reads still answer.
        assertEquals(page(3), open("d").opened)
        settle()
        assertEquals(1, writer.reservations)
        // A stored command's caller hears Queued promptly, and nothing is sent.
        val result = CompletableDeferred<CommandOutcome>()
        later { result.complete(c.command(op("set_status", "status" to "on_hold"))) }
        settle()
        assertEquals(CommandOutcome.QUEUED, result.getCompleted())
        assertEquals(emptyList<Any>(), writes())
        advance(ReadingEngine.STORAGE_RETRY)
        assertEquals(2, writer.reservations)
        writer.full = false
        advance(ReadingEngine.STORAGE_RETRY)
        assertEquals(page(3), server.stateOf("c").progress)
        assertEquals(1, ops().count { it == "set_status" })
        assertEquals("on_hold", server.stateOf("c").status)
    }

    /** A load queued behind a guarded write that reaches the refused reservation isn't starved by it. */
    @Test
    fun `a refused writer reservation doesn't starve a load queued behind the write`() = engineTest {
        server.series("s", "a", "b")
        server.set("a") { copy(status = "reading", progress = page(2)) }
        server.set("d") { copy(status = "reading", progress = page(3)) }
        val (a) = open("a")
        writer.full = true
        // Held at the write's own pre-send volumes (the second request), with the load queued behind it.
        var volumes = 0
        server.holds = { it.path == "volumes" && ++volumes == 2 }
        server.holding = true
        val series = Content("s", "Tin Lantern", ContentType.COMIC_SERIES)
        later { engine.seriesCommand(series, SeriesAction.MARK_SERIES_COMPLETED, includeUnread = true) }
        settle()
        val reader = attach("d")
        settle()
        server.release()
        settle()
        assertEquals(page(3), reader.loading.await())
        assertEquals(emptyList<Any>(), writes())
        assertEquals(1, writer.reservations)
        advance(ReadingEngine.STORAGE_RETRY)
        assertEquals(2, writer.reservations)
    }

    @Test
    fun `commands fail at entry while storage is full, nothing changes, and the first one after space is back goes`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync) = open("c")
        server.unreachable = true
        sync.finish(END)
        settle()
        assertEquals(listOf(OpKind.FINISH), store.ops.map { it.kind })
        faults.fail = { true }
        read(sync, 3)
        assertTrue(sync.view.value.storageFull)
        // A Clear would displace the stored finish: it fails first, and the reading stays as it is.
        assertTrue(runCatching { sync.command(op("clear")) }.exceptionOrNull() is StorageFullException)
        assertEquals(listOf(OpKind.FINISH), store.ops.map { it.kind })
        // A reader's Reset shows the failure at once, with Retry, and doesn't place the reader.
        sync.resetAndReadAgain()
        settle()
        answer(PromptChoice.CONFIRM)
        assertTrue(failure(SyncAction.CLEAR).action != null)
        assertTrue(notices.none { it.notice is SyncNotice.Done })
        // A prompt that is answered goes at once, and the slot is free again.
        assertNull(prompt())

        faults.fail = null
        val result = CompletableDeferred<CommandOutcome>()
        later { result.complete(sync.command(op("clear"))) }
        settle()
        assertEquals(CommandOutcome.QUEUED, result.getCompleted())
        // The clear took the finish and the position with it, in one commit.
        assertEquals(listOf("clear"), store.ops.map { it.payload.string("op") })
    }

    /** An Undo made while storage is known full fails at once, and Retry works once space is back. */
    @Test
    fun `an undo made while storage is full shows its failure at once`() = engineTest {
        server.set("c") { copy(status = "on_hold", progress = page(4)) }
        val (sync) = open("c")
        read(sync, 5)
        val undo = offer(Undoable.MOVED_TO_READING).action!!
        faults.fail = { true }
        read(sync, 6)
        val sent = server.sent.size
        undo()
        settle()
        assertEquals(sent, server.sent.size)
        faults.fail = null
        failure(SyncAction.UNDO).action!!()
        settle()
        assertEquals("on_hold", server.stateOf("c").status)
    }

    /**
     * A command queued just before the first refusal is answered Unsaved, stays in memory, and is
     * stored and sent when space is back. A reader's Clear that raced says so, and claims no Done.
     */
    @Test
    fun `a command that raced the first refusal is Unsaved and is sent once space is back, with no Done before its answer`() = engineTest {
        val (sync) = open("c")
        faults.fail = { change -> change.ops.any { it.id == 0L } }
        val result = CompletableDeferred<CommandOutcome>()
        later { result.complete(sync.command(op("set_status", "status" to "on_hold"))) }
        settle()
        assertEquals(CommandOutcome.UNSAVED, result.getCompleted())
        assertEquals(emptyList<Any>(), writes())
        assertEquals(emptyList<Any>(), store.ops)
        faults.fail = null
        advance(ReadingEngine.STORAGE_RETRY)
        assertEquals("on_hold", server.stateOf("c").status)
        assertEquals(emptyList<Any>(), store.ops)

        // A Reset that raced: no placement, no Done, no Retry; it goes when space is back.
        faults.fail = { change -> change.ops.any { it.id == 0L } }
        sync.resetAndReadAgain()
        settle()
        answer(PromptChoice.CONFIRM)
        assertTrue(notices.any { it.notice is SyncNotice.NotSaved })
        assertTrue(notices.none { it.notice is SyncNotice.Failed || it.notice is SyncNotice.Done })
        server.holding = true
        faults.fail = null
        advance(ReadingEngine.STORAGE_RETRY)
        assertEquals(1, server.inFlight)
        assertTrue(notices.none { it.notice is SyncNotice.Done })
        server.release()
        settle()
        assertEquals(listOf("set_status", "clear"), ops())
        assertTrue(notices.none { it.notice is SyncNotice.Done || it.notice is SyncNotice.Failed })
        assertEquals(emptyList<Any>(), store.ops)
    }

    /** The replacing command is Unsaved, in memory only: a kill before storage recovers loses it, and the stored finish stays. */
    @Test
    fun `an unsaved command that replaces a stored finish is lost when the engine stops, and the finish remains`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync) = open("c")
        server.unreachable = true
        sync.finish(END)
        settle()
        assertEquals(listOf(OpKind.FINISH), store.ops.map { it.kind })
        val sent = writes().size
        faults.fail = { change -> change.ops.any { it.kind == OpKind.COMMAND } }
        val result = CompletableDeferred<CommandOutcome>()
        later { result.complete(sync.command(op("clear"))) }
        settle()
        assertEquals(CommandOutcome.UNSAVED, result.getCompleted())
        // The clear was never sent.
        assertEquals(sent, writes().size)
        assertEquals(listOf(OpKind.FINISH), store.ops.map { it.kind })
        // Killed while commits are still refused.
        engine.stop()
        settle()
        assertEquals(listOf(OpKind.FINISH), store.ops.map { it.kind })
        restart {
            faults.fail = null
            server.unreachable = false
        }
        // The stored finish is sent; the unsaved clear is gone and never goes.
        assertEquals(emptyList<Any>(), store.ops)
        // The unreachable first attempt, then the resend: never the clear.
        assertEquals(listOf("finish", "finish"), ops())
    }

    /** The answer's deletion is refused: its held-series prompt opens no hidden dialog, and reading goes on. */
    @Test
    fun `an answer that can't be stored doesn't hold the dialog slot, and its prompt shows once space is back`() = engineTest {
        server.series("s", "c")
        server.set("s") { copy(status = "on_hold") }
        server.set("c") { copy(status = "dropped") }
        server.set("d") { copy(status = "reading", progress = page(1)) }
        val (sync) = open("c")
        faults.fail = { it.deleteOps.isNotEmpty() }
        sync.finish(END)
        settle()
        assertNull(prompt())
        assertEquals(page(1), open("d").opened)
        faults.fail = null
        advance(ReadingEngine.STORAGE_RETRY)
        assertEquals(SyncPrompt.SeriesHeld("on_hold", Undoable.MARKED_COMPLETED), prompt())
    }

    /** A series command queued behind a write whose answer is the first refusal fails without touching reading. */
    @Test
    fun `a series command queued behind a write whose answer can't be stored changes nothing`() = engineTest {
        server.series("s", "a", "b")
        server.set("a") { copy(status = "reading", progress = page(2)) }
        val (a) = open("a")
        server.holding = true
        read(a, 3)
        val result = CompletableDeferred<Result<CommandOutcome>>()
        val series = Content("s", "Tin Lantern", ContentType.COMIC_SERIES)
        later { result.complete(runCatching { engine.seriesCommand(series, SeriesAction.MARK_SERIES_COMPLETED, includeUnread = true) }) }
        settle()
        faults.fail = { it.deleteOps.isNotEmpty() }
        server.release()
        settle()
        assertTrue(result.getCompleted().exceptionOrNull() is StorageFullException)
        assertTrue(server.sent.none { it.body?.string("action") != null })
        assertTrue(store.ops.none { it.kind == OpKind.SERIES })
    }

    /** Review works on a full disk: it reads and asks, its outcome and end are set, and the choice joins the refused batch. */
    @Test
    fun `a review under storage parking reads, asks, and ends`() = engineTest(
        before = {
            val acked = ReadingState("srv:g-before", "reading", progress = page(2))
            store.commit(Change(lanes = listOf(Lane("g", type = ContentType.COMIC, title = "Title g", pageCount = LAST + 1, state = acked, here = page(5), needsReview = true)), ops = listOf(Op(contentId = "g", kind = OpKind.POSITION, payload = page(5), sealed = true))))
            server.set("g") { copy(status = "reading", progress = page(8)) }
        },
    ) {
        faults.fail = { true }
        // Something to refuse, so the engine is parked for storage.
        server.set("h") { copy(status = "reading", progress = page(1)) }
        read(open("h").sync, 2)
        val review = review("g")
        assertEquals(ConflictKind.MOVED, (review.prompt.value as SyncPrompt.Conflict).kind)
        review.answer(PromptChoice.GO)
        settle()
        assertNull(review.prompt.value)
        assertEquals(true, review.ended.value)
        // The choice joins the refused batch: after a kill the stored lane is still held, with its position.
        assertEquals(true, store.lane("g")?.needsReview)
        faults.fail = null
        advance(ReadingEngine.STORAGE_RETRY)
        assertEquals(page(8), store.lane("g")?.here)
        assertEquals(false, store.lane("g")?.needsReview)
    }

    /** A stored command whose last commit storage refuses, then the engine stops: by `stop()` or by its parent. */
    @Test
    fun `a stop or a cancelled parent while an answer can't be stored answers Queued, and the next engine sends it`() =
        eachCase(listOf(Refused.ACK to false, Refused.ACK to true, Refused.UNREACHABLE to false, Refused.UNREACHABLE to true)) { (refused, cancelParent) ->
            storedThenStopped(refused, cancelParent)
        }

    private enum class Refused { ACK, UNREACHABLE }

    private fun storedThenStopped(refused: Refused, cancelParent: Boolean) = engineTest {
        val parent = Job(testScope.backgroundScope.coroutineContext[Job])
        engine = newEngine(CoroutineScope(testScope.backgroundScope.coroutineContext + parent)).also { it.start() }
        val (sync) = open("c")
        server.holding = true
        val result = CompletableDeferred<Result<CommandOutcome>>()
        later { result.complete(runCatching { sync.command(op("set_status", "status" to "on_hold")) }) }
        settle()
        if (refused == Refused.ACK) {
            faults.fail = { it.deleteOps.isNotEmpty() }
        } else {
            // The op goes back to the queue: its attempt can't be stored, and its caller's QUEUED waits for that.
            faults.fail = { change -> change.ops.any { it.attempts > 0 } }
            server.unreachable = true
        }
        server.release()
        settle()
        assertEquals(if (refused == Refused.ACK) "on_hold" else null, server.stateOf("c").status)
        assertFalse(result.isCompleted)
        if (cancelParent) parent.cancel() else engine.stop()
        settle()
        assertEquals(CommandOutcome.QUEUED, result.getCompleted().getOrThrow())
        assertEquals(1, store.ops.size)
        restart {
            faults.fail = null
            server.unreachable = false
        }
        assertEquals(emptyList<Any>(), store.ops)
        // Every send went as the one (writer, seq) pair, and the server applied it once.
        assertEquals(1, writes().map { it.long("seq") }.distinct().size)
        assertEquals("on_hold", server.stateOf("c").status)
    }

    /** A dispatcher that runs only when told. */
    private class Gate : CoroutineDispatcher() {
        private val queue = ArrayDeque<Runnable>()

        override fun dispatch(context: CoroutineContext, block: Runnable) {
            queue += block
        }

        fun run() {
            while (queue.isNotEmpty()) queue.removeFirst().run()
        }
    }
}

/**
 * A commit the store refuses, at each step of a position's way out: nothing goes before it is
 * stored, nothing is lost, and a new engine on the same store sends what is owed once.
 */
@RunWith(Parameterized::class)
class EngineRefusedCommitTest(private val step: String, private val killed: Boolean) : EngineTest() {
    @Test
    fun `a refused commit sends nothing early and loses nothing`() = engineTest {
        val (sync) = open("c")
        faults.fail = when (step) {
            "enqueue" -> { change -> change.ops.any { it.id == 0L } }
            "send mark" -> { change -> change.ops.any { it.sentSeq != null } }
            else -> { change -> change.deleteOps.isNotEmpty() }
        }
        read(sync, 1)
        // While storage parks the pump, turns merge into the one unsent position.
        val last = if (step == "enqueue") 3 else 1
        for (n in 2..last) read(sync, n)
        // Nothing leaves before its mark is stored; an answer's commit is all that can be refused after.
        assertEquals(if (step == "answer") 1 else 0, writes().size)
        assertEquals(emptyList<Any>(), changes)
        if (killed) {
            restart { faults.fail = null }
        } else {
            faults.fail = null
            advance(ReadingEngine.STORAGE_RETRY)
            restart()
        }
        settle()
        assertEquals(page(last), server.stateOf("c").progress)
        assertEquals(emptyList<Any>(), store.ops)
        if (step == "enqueue") assertEquals(listOf(page(3)), writes().map { it.progress() })
        // Every send of it went as the one (writer, seq) pair, and the server applied it once.
        val pairs = writes().map { it.string("writer_id") to it.long("seq") }.distinct()
        assertEquals(1, pairs.size)
        assertEquals(pairs.single().let { (w, seq) -> "$w:$seq" }, server.stateOf("c").revision)
    }

    companion object {
        @JvmStatic
        @Parameterized.Parameters(name = "{0}, killed {1}")
        fun cases() = listOf(
            arrayOf<Any>("enqueue", false),
            arrayOf<Any>("send mark", false),
            arrayOf<Any>("send mark", true),
            arrayOf<Any>("answer", false),
            arrayOf<Any>("answer", true),
        )
    }
}
