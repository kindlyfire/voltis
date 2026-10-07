package me.tijlvdb.voltis.data.reading

import me.tijlvdb.voltis.data.content.CatalogChange.ReadingChanged
import me.tijlvdb.voltis.domain.reading.Change
import me.tijlvdb.voltis.domain.reading.ConflictKind
import me.tijlvdb.voltis.domain.reading.Lane
import me.tijlvdb.voltis.domain.reading.Op
import me.tijlvdb.voltis.domain.reading.OpKind
import me.tijlvdb.voltis.domain.reading.PositionLabel
import me.tijlvdb.voltis.domain.reading.PromptChoice
import me.tijlvdb.voltis.domain.reading.ReadingSession
import me.tijlvdb.voltis.domain.reading.SyncPrompt
import org.junit.Assert.assertEquals
import org.junit.Test

/** Not in the web: a new engine over the same store, as after the process was killed (P2 §5, How each web behaviour maps). */
class EnginePersistenceTest : EngineTest() {
    @Test
    fun `sends a position the debounce hadn't sealed`() = engineTest {
        val (sync) = open("c")
        sync.moved(page(3))
        settle()
        restart()
        assertEquals(page(3), server.stateOf("c").progress)
    }

    @Test
    fun `sends an op that was out and didn't land again with its seq, and it applies`() = engineTest {
        val (sync) = open("c")
        server.holding = true
        read(sync, 3)
        restart { server.holding = false }
        val (first, repeat) = writes()
        assertEquals(first.long("seq"), repeat.long("seq"))
        assertEquals(page(3), server.stateOf("c").progress)
        assertEquals(emptyList<Any>(), store.ops)
    }

    /** A position, and a finish. */
    @Test
    fun `answers an op that was out and landed with none, applying nothing twice`() {
        val writes: List<EngineTest.Harness.(ReadingSession) -> Unit> = listOf(
            { read(it, 3) },
            {
                it.finish(END)
                settle()
            },
        )
        eachCase(writes) { landedThenKilled(it) }
    }

    private fun landedThenKilled(write: EngineTest.Harness.(ReadingSession) -> Unit) = engineTest {
        val (sync) = open("c")
        server.loseAck = true
        write(sync)
        val landed = server.stateOf("c")
        changes.clear()
        restart()
        assertEquals(landed, server.stateOf("c"))
        assertEquals(1, writes().map { it.long("seq") }.distinct().size)
        assertEquals(listOf(ReadingChanged("c", "c")), changes)
        assertEquals(emptyList<Any>(), store.ops)
    }

    @Test
    fun `sends no sibling volume before an op whose result is unknown, but another series' op`() {
        server.series("s", "c", "d")
        server.refuse = { it.id == "c" }
        engineTest(
            before = {
                val ops = listOf(
                    Op(contentId = "c", kind = OpKind.POSITION, payload = page(1), sealed = true, sentSeq = 7, sentWriter = writer.writerId),
                    Op(contentId = "d", kind = OpKind.POSITION, payload = page(2), sealed = true),
                    Op(contentId = "e", kind = OpKind.POSITION, payload = page(3), sealed = true),
                )
                store.commit(Change(lanes = listOf(Lane("c", parentId = "s"), Lane("d", parentId = "s"), Lane("e")), ops = ops))
            },
        ) {
            settle()
            // The unknown one, then again for its sibling, which fails with it; the other series goes.
            assertEquals(listOf("c", "c", "e"), server.sent.filter { it.body != null }.map { it.id })
            assertEquals(true, store.lane("d")?.failed)
            server.refuse = { false }
            engine.drain()
            assertEquals(listOf("c", "c", "e", "c", "d"), server.sent.filter { it.body != null }.map { it.id })
            assertEquals(7L, writes()[3].long("seq"))
            assertEquals(page(2), server.stateOf("d").progress)
        }
    }

    @Test
    fun `asks again after a kill with the conflict dialog open`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync) = open("c")
        sync.moved(page(5))
        server.set("c") { copy(progress = page(79)) }
        sync.check()
        settle()
        assertEquals(ConflictKind.MOVED, (prompt() as SyncPrompt.Conflict).kind)
        restart()
        assertEquals(true, store.lane("c")?.needsReview)
        val again = attach("c")
        settle()
        assertEquals(SyncPrompt.Conflict(ConflictKind.MOVED, PositionLabel.Page(6), PositionLabel.Page(80)), prompt())
        answer(PromptChoice.STAY)
        again.loading.await()
        assertEquals(page(5), server.stateOf("c").progress)
    }

    @Test
    fun `keeps tracking and failures`() = engineTest(
        before = {
            store.commit(Change(lanes = listOf(Lane("c", tracking = false, failures = 2))))
        },
    ) {
        val (sync) = open("c")
        assertEquals(false to 2, sync.view.value.let { it.tracking to it.failures })
        read(sync, 3)
        assertEquals(emptyList<Any>(), writes())
    }
}
