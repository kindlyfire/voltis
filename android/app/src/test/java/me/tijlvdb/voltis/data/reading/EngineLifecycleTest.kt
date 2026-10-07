package me.tijlvdb.voltis.data.reading

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import me.tijlvdb.voltis.domain.reading.Change
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.reading.ConflictKind
import me.tijlvdb.voltis.domain.reading.EmptyProgress
import me.tijlvdb.voltis.domain.reading.Lane
import me.tijlvdb.voltis.domain.reading.Op
import me.tijlvdb.voltis.domain.reading.OpKind
import me.tijlvdb.voltis.domain.reading.PromptChoice
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.ReviewOutcome
import me.tijlvdb.voltis.domain.reading.SyncPrompt
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Not in the web: the engine's start, stop, sessions and writer identity across restarts. */
class EngineLifecycleTest : EngineTest() {
    /** Not landed, landed, and overtaken by the new writer: the repeat's base is stale, and the 409 is our own. */
    @Test
    fun `repeats an unknown op as the writer it went as, after the identity changed`() {
        val unchanged = listOf(Triple(OLD, 500L, "srv:1"), Triple(NEW, 1L, "$OLD:500"))
        eachCase(
            listOf(
                Triple(null, 0L, unchanged),
                Triple("$OLD:500", 0L, unchanged),
                Triple("$NEW:3", 3L, listOf(Triple(OLD, 500L, "srv:1"), Triple(NEW, 4L, "$NEW:3"), Triple(NEW, 5L, "$NEW:4"))),
            ),
        ) { (meanwhile, counter, sent) -> repeatAfterNewIdentity(meanwhile, counter, sent) }
    }

    /**
     * An op sent as [OLD] with seq 500, its result unknown, found by an engine whose writer is now [NEW]
     * at [counter]. [meanwhile] is the revision the row has before the repeat, if it changed. [sent] is
     * every write, as (writer, seq, base), up to a page turn after.
     */
    private fun repeatAfterNewIdentity(meanwhile: String?, counter: Long, sent: List<Triple<String?, Long?, String?>>) {
        writer = FakeWriter(NEW).apply { seq = counter }
        engineTest(
            before = {
                server.set("c") { copy(status = "reading", progress = page(2)) }
                val unknown = Op(contentId = "c", kind = OpKind.POSITION, payload = page(5), sealed = true, sentSeq = 500, sentWriter = OLD)
                store.commit(Change(lanes = listOf(Lane("c", state = server.stateOf("c"))), ops = listOf(unknown)))
                meanwhile?.let { server.set("c", it) { copy(progress = page(if (it.startsWith(OLD)) 5 else 4)) } }
            },
        ) {
            settle()
            // What was owed is on the server, adopted, and gone from the outbox, with nothing to ask.
            assertEquals(page(5), server.stateOf("c").progress)
            assertEquals(emptyList<Any>(), store.ops)
            assertEquals(server.stateOf("c").revision, store.lane("c")?.state?.revision)
            assertNull(prompt())
            // The new writer's next seq isn't taken for a repeat of an older write.
            val (sync) = open("c")
            read(sync, 6)
            assertEquals(sent, writes().map { Triple(it.string("writer_id"), it.long("seq"), it.string("base_revision")) })
            assertEquals(page(6), server.stateOf("c").progress)
            assertNull(prompt())
        }
    }

    @Test
    fun `fails callers until a retried start has loaded the outbox`() {
        faults.failLoad = true
        engineTest(before = { store.commit(Change(lanes = listOf(Lane("c")), ops = listOf(Op(contentId = "c", kind = OpKind.POSITION, payload = page(3))))) }) {
            val reader = attach("c")
            settle()
            assertTrue(reader.loading.isCompleted && runCatching { reader.loading.await() }.isFailure)
            assertEquals(emptyList<Any>(), server.sent)
            faults.failLoad = false
            engine.retryStart()
            open("c")
            // The stored position goes before the reader's read.
            assertEquals(listOf("reading", "get"), server.sent.map { it.path })
            assertEquals(page(3), server.stateOf("c").progress)
        }
    }

    @Test
    fun `a reader that leaves takes its dialog, also when another reader has the item`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val first = open("c")
        val other = open("d")
        first.sync.moved(page(5))
        server.set("c") { copy(progress = page(79)) }
        first.sync.check()
        settle()
        assertEquals(ConflictKind.MOVED, (prompt() as SyncPrompt.Conflict).kind)
        // Behind the dialog: another lane's write, and a second reader's load of the same item.
        read(other.sync, 3)
        val second = attach("c")
        settle()
        assertEquals(EmptyProgress, server.stateOf("d").progress)

        first.sync.detach()
        settle()
        assertNull(first.sync.prompt.value)
        assertEquals(page(3), server.stateOf("d").progress)
        // The second reader's load asks again.
        assertEquals(ConflictKind.MOVED, (second.sync.prompt.value as SyncPrompt.Conflict).kind)
        second.sync.answer(PromptChoice.STAY)
        advance(1000)
        assertEquals(page(5), second.loading.await())
        assertEquals(page(5), server.stateOf("c").progress)
    }

    @Test
    fun `stop returns once the request out has finished`() = engineTest {
        val (sync) = open("c")
        server.holding = true
        read(sync, 1)
        assertEquals(1, server.inFlight)
        engine.stop()
        assertEquals(0, server.inFlight)
        // An engine that never started stops too.
        newEngine().stop()
    }

    @Test
    fun `a rebase read that fails is not read again at once`() {
        server.series("s", "c")
        server.failing = true
        engineTest(
            before = {
                val command = Op(contentId = "c", kind = OpKind.COMMAND, payload = op("set_status", "status" to "on_hold"), after = listOf("c"))
                store.commit(Change(lanes = listOf(Lane("c", parentId = "s", rebase = true)), ops = listOf(command)))
            },
        ) {
            settle()
            advance(60_000)
            assertEquals(listOf("get"), server.sent.map { it.path })
            assertEquals(listOf(OpKind.COMMAND), store.ops.map { it.kind })
            assertEquals(true, store.lane("c")?.rebase)
        }
    }

    @Test
    fun `stops after its parent was cancelled, answering who waits`() = engineTest {
        val parent = Job(testScope.backgroundScope.coroutineContext[Job])
        engine = newEngine(CoroutineScope(testScope.backgroundScope.coroutineContext + parent)).also { it.start() }
        val (sync) = open("c")
        server.holding = true
        val result = CompletableDeferred<Result<CommandOutcome>>()
        later { result.complete(runCatching { sync.command(op("set_status", "status" to "on_hold")) }) }
        settle()
        assertEquals(1, server.inFlight)
        parent.cancel()
        settle()
        // Its row stays for the account's next engine.
        assertEquals(CommandOutcome.QUEUED, result.getCompleted().getOrThrow())
        // Two stops at once both return.
        val stops = List(2) { testScope.async { engine.stop() } }
        settle()
        assertTrue(stops.all { it.isCompleted })
        assertTrue(runCatching { sync.load() }.isFailure)
    }

    @Test
    fun `a throwing effect is reported and the effects after it still run, and an escaping error is fatal`() = engineTest {
        engine.stop()
        engine = newEngine(onGone = { id, _ -> gone += id; throw IllegalStateException("boom") }).also { it.start() }
        server.failWith = { if (it.id == "x") ReadingFailure.Gone("gone") else null }
        val reader = attach("x")
        settle()
        // The hook threw after the commit: reported, the reader's load still failed, and the engine serves the next event.
        assertEquals(listOf("x"), gone)
        assertEquals(1, errors.size)
        errors.clear()
        assertTrue(runCatching { reader.loading.await() }.exceptionOrNull() is ReadingFailure.Gone)
        val (sync) = open("c")
        assertTrue(crashes.isEmpty())

        // Something no step guards against: the loop ends, the process would end, and callers fail.
        val (other) = open("d")
        faults.fail = { throw AssertionError("boom") }
        // A review still reading ends failed with it.
        server.holding = true
        val review = review("c")
        other.moved(page(3))
        settle()
        assertTrue(errors.any { it is AssertionError })
        assertTrue(crashes.single() is AssertionError)
        errors.clear()
        assertTrue(runCatching { sync.load() }.isFailure)
        assertTrue(review.ended.value)
        server.release()
        engine.stop()
    }

    @Test
    fun `a review ends when its engine can't start or stops, and a failed start is tried again for it`() = engineTest(before = { faults.failLoad = true }) {
        val failed = review("c")
        assertEquals(ReviewOutcome.FAILED, failed.outcome.value)
        assertTrue(failed.ended.value)

        // The start is retried ahead of it: nothing to read for an item that has no lane.
        faults.failLoad = false
        val resolved = review("c")
        assertEquals(ReviewOutcome.RESOLVED, resolved.outcome.value)
        assertTrue(resolved.ended.value)

        // Still reading when the engine stops: it fails and ends.
        server.set("d") { copy(status = "reading", progress = page(2)) }
        open("d")
        server.holding = true
        val reading = review("d")
        engine.stop()
        assertEquals(ReviewOutcome.FAILED, reading.outcome.value)
        assertTrue(reading.ended.value)
    }

    private companion object {
        const val OLD = "toldwriter0000000"
        const val NEW = "tnewwriter0000000"
    }
}
