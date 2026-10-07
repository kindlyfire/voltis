package me.tijlvdb.voltis.data.reading

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.content.CatalogSignal
import me.tijlvdb.voltis.data.db.OpenAccount
import me.tijlvdb.voltis.domain.catalog.BulkWhat
import me.tijlvdb.voltis.domain.catalog.Selected
import me.tijlvdb.voltis.domain.catalog.clearPlan
import me.tijlvdb.voltis.domain.catalog.statusPlan
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.reading.ReadingCommands
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.SyncCommand
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.sync.NoticeDetail
import me.tijlvdb.voltis.domain.sync.NoticeKind
import me.tijlvdb.voltis.domain.sync.StoredNotice
import me.tijlvdb.voltis.domain.sync.SyncNotices
import me.tijlvdb.voltis.ui.grid.BatchFollower
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class BulkRunnerTest {
    private class Store(override val account: String) : OpenAccount

    /** Each command waits for the test's answer: an outcome, or what it throws. A success changes the item, as the engine does. */
    private class Commands(private val events: CatalogEvents) : ReadingCommands {
        val answers = Channel<Result<CommandOutcome>>(Channel.UNLIMITED)
        var origin = 0L

        /** The batch of each command made. */
        val made = mutableListOf<Long>()

        private suspend fun next(id: String): CommandOutcome {
            made += origin
            val outcome = answers.receive().getOrThrow()
            if (outcome == CommandOutcome.APPLIED) events.emit(CatalogChange.ReadingChanged(id, id), origin)
            return outcome
        }

        override suspend fun setStatus(content: Content, status: String?) = next(content.id)

        override suspend fun clear(content: Content) = next(content.id)

        override suspend fun markSeriesCompleted(series: Content, includeUnread: Boolean) = next(series.id)

        override suspend fun markThrough(series: Content, untilId: String) = error("unused")
    }

    private class Notices : SyncNotices {
        val stored = mutableListOf<Pair<StoredNotice, Boolean>>()

        /** The store each notice was written to. */
        val into = mutableListOf<OpenAccount>()
        var attempts = 0

        /** While set, an insert waits for it. */
        var gate: CompletableDeferred<Unit>? = null

        /** Which inserts throw. */
        var fails: (StoredNotice) -> Boolean = { false }

        override suspend fun insert(store: OpenAccount, notice: StoredNotice, announce: Boolean) {
            attempts++
            gate?.await()
            check(!fails(notice)) { "disk full" }
            stored += notice to announce
            into += store
        }

        override fun observe(): Flow<List<StoredNotice>> = flowOf(stored.map { it.first })

        override val fresh: SharedFlow<StoredNotice> = MutableSharedFlow()
    }

    private val items = (1..5).map { Selected(Content("c_$it", "Lantern Moth $it", ContentType.COMIC)) }
    private fun touched(n: Int) = items.take(n).map { CatalogChange.ReadingChanged(it.content.id, it.content.id) }

    private inner class Fixture(val test: TestScope) {
        val storeA = Store("a")
        val storeB = Store("b")
        val opened = MutableStateFlow<OpenAccount?>(storeA)
        val events = CatalogEvents()
        val commands = Commands(events)
        val notices = Notices()
        val calls = mutableListOf<Pair<String, Long>>()
        val signals = mutableListOf<CatalogSignal>()
        // Cancelled with the test: a failed assertion mustn't leave a retrying batch to run forever.
        private val scope = CoroutineScope(SupervisorJob() + StandardTestDispatcher(test.testScheduler))
        val runner = BulkRunner(
            { account, origin ->
                calls += account to origin
                commands.origin = origin
                commands
            },
            notices,
            events,
            scope,
            opened,
            { it.message.orEmpty() },
        ) { 42 }

        init {
            // The flow has no replay: subscribed before the first batch.
            test.backgroundScope.launch {
                try {
                    events.signals.collect { signals += it }
                } finally {
                    scope.cancel()
                }
            }
            test.runCurrent()
        }

        fun answer(results: List<Result<CommandOutcome>>) = results.forEach { commands.answers.trySend(it) }
        val applied = Result.success(CommandOutcome.APPLIED)
        fun refused() = Result.failure<CommandOutcome>(ReadingFailure.ServerError(500, "boom"))

        fun clear(n: Int, account: String = "a") = runner.start(account, BulkWhat.Clear, clearPlan(items.take(n)))
    }

    @Test
    fun runsStoresAndQueues() = runTest {
        val f = Fixture(this)
        val what = BulkWhat.Status(ReadingStatus.ON_HOLD)
        val id = f.runner.start("a", what, statusPlan(items, ReadingStatus.ON_HOLD, false))!!
        assertNull(f.runner.start("a", BulkWhat.Clear, clearPlan(items)))
        assertNull(f.runner.start("other", BulkWhat.Clear, clearPlan(items)))

        f.notices.fails = { it.kind == NoticeKind.GONE }
        f.answer(listOf(f.applied, f.refused()))
        runCurrent()
        // Stored at once, unannounced: the count and Needs attention agree.
        assertEquals(BulkState(id, what, 5, done = 1, failed = 1), f.runner.state.value)
        assertEquals(
            listOf(StoredNotice(0, "c_2", "Lantern Moth 2", NoticeKind.REFUSED, NoticeDetail(message = "boom"), 42) to false),
            f.notices.stored,
        )

        f.answer(
            listOf(
                Result.success(CommandOutcome.QUEUED),
                Result.failure(ReadingFailure.Gone("gone")),
                Result.failure(ReadingFailure.ChangedElsewhere(SyncCommand.CLEAR, uncertain = true)),
            ),
        )
        runCurrent()
        // Every command ran and the batch ended for the screens; the Gone notice isn't stored yet, so no summary.
        assertEquals(BulkState(id, what, 5, done = 2, failed = 3, unsaved = 1), f.runner.state.value)
        assertTrue(f.runner.state.value!!.saving)
        assertTrue(f.runner.summaries.value.isEmpty())
        // Bound to the account and tagged with the batch, one command each.
        assertEquals(listOf("a" to id), f.calls)
        assertEquals(List(5) { id }, f.commands.made)
        assertEquals(CatalogSignal.BatchEnded(id, touched(5)), f.signals.last())
        assertTrue(f.signals.dropLast(1).all { it is CatalogSignal.Changed && it.batch == id })

        f.notices.fails = { false }
        advanceTimeBy(1_000)
        runCurrent()
        assertNull(f.runner.state.value)
        assertEquals(
            listOf(
                StoredNotice(0, "c_2", "Lantern Moth 2", NoticeKind.REFUSED, NoticeDetail(message = "boom"), 42),
                StoredNotice(0, "c_5", "Lantern Moth 5", NoticeKind.CHANGED_ELSEWHERE, NoticeDetail(command = SyncCommand.CLEAR, uncertain = true), 42),
                StoredNotice(0, "c_4", "Lantern Moth 4", NoticeKind.GONE, NoticeDetail(), 42),
            ).map { it to false },
            f.notices.stored,
        )
        assertEquals(listOf(BulkSummary(id, f.storeA, what, done = 2, failed = 3)), f.runner.summaries.value)
        assertEquals(5, f.commands.made.size)

        // A batch that ends before its follower looks: the follower still sees it end, once.
        var ended = 0
        val follower = BatchFollower(backgroundScope, f.runner.state) { ended++ }
        runCurrent()
        f.answer(listOf(f.applied, f.applied))
        val second = f.clear(2)!!
        follower.follow(BulkState(second, BulkWhat.Clear, 2))
        runCurrent()
        assertEquals(1, ended)
        assertNull(follower.state.value)
        assertEquals(listOf(id, second), f.runner.summaries.value.map { it.id })
        f.runner.acknowledge(id)
        assertEquals(listOf(second), f.runner.summaries.value.map { it.id })

        // The follower's wake-ups carry stale values while the runner's current one has moved on: it acts on the current ones.
        val live = MutableStateFlow<BulkState?>(null)
        val delivered = MutableStateFlow<BulkState?>(null)
        var selection: String? = "first"
        val delayed = BatchFollower(backgroundScope, GatedState(live, delivered)) {
            ended++
            selection = null
        }
        runCurrent()
        // Running, but the runner's update isn't delivered yet: no premature end.
        val one = BulkState(10, BulkWhat.Clear, 2)
        live.value = one
        delayed.follow(one)
        runCurrent()
        assertEquals(1, ended)
        assertEquals(one, delayed.state.value)
        delivered.value = one
        runCurrent()
        assertEquals(1, ended)
        // The batch ends, its update not yet delivered; Hide, then a new selection; the old update arrives: nothing ends, no progress returns.
        live.value = null
        delayed.forget()
        runCurrent()
        selection = "second"
        delivered.value = null
        runCurrent()
        assertEquals(1, ended)
        assertEquals("second", selection)
        assertNull(delayed.state.value)
    }

    @Test
    fun notSavedCommandsAreNeitherDoneNorFailedNorResent() {
        val unsaved = Result.success(CommandOutcome.UNSAVED)
        val queued = Result.success(CommandOutcome.QUEUED)
        // Name, the outcomes in order, the summary's done / failed / notSaved.
        class Case(val name: String, val outcomes: (Fixture) -> List<Result<CommandOutcome>>, val done: Int, val failed: Int, val notSaved: Int)
        val cases = listOf(
            Case("all unsaved", { listOf(unsaved, unsaved, unsaved) }, 0, 0, 3),
            Case("mixed", { listOf(it.applied, unsaved, it.refused(), queued) }, 2, 1, 1),
        )
        for (case in cases) {
            runTest {
                val f = Fixture(this)
                val outcomes = case.outcomes(f)
                f.answer(outcomes)
                val id = f.clear(outcomes.size)!!
                runCurrent()
                // Time passes and space comes back: nothing is made again.
                advanceTimeBy(60_000)
                runCurrent()
                assertNull(case.name, f.runner.state.value)
                assertEquals(case.name, outcomes.size, f.commands.made.size)
                assertEquals(case.name, listOf(BulkSummary(id, f.storeA, BulkWhat.Clear, case.done, case.failed, case.notSaved)), f.runner.summaries.value)
                // Only the refusals are notices: an unsaved command is not a failure to review or retry.
                assertEquals(case.name, case.failed, f.notices.stored.size)
            }
        }
    }

    /** A runner whose collectors see [delivered], while [value] is already [live]'s: delivery lags the state. */
    private class GatedState(private val live: StateFlow<BulkState?>, private val delivered: StateFlow<BulkState?>) : StateFlow<BulkState?> by delivered {
        override val value get() = live.value
    }

    private data class Row(val name: String, val script: suspend Fixture.() -> Unit)

    @Test
    fun teardownAndAccountChange() {
        val rows = listOf(
            Row("a: stopped before it ran") {
                clear(2)
                runner.stop()
                assertTrue(signals.isEmpty())
                assertTrue(runner.summaries.value.isEmpty())
            },
            Row("b: stopped during a command") {
                clear(2)
                test.runCurrent()
                runner.stop()
                assertEquals(1, signals.count { it is CatalogSignal.BatchEnded })
                assertTrue(notices.stored.isEmpty())
                assertTrue(runner.summaries.value.isEmpty())
            },
            Row("c: the account changes during an insert") {
                notices.gate = CompletableDeferred()
                answer(listOf(refused(), refused()))
                clear(2)
                test.runCurrent()
                opened.value = storeB
                notices.gate!!.complete(Unit)
                test.runCurrent()
                assertEquals(1, commands.made.size)
                assertEquals(1, signals.count { it is CatalogSignal.BatchEnded })
                assertTrue(runner.summaries.value.isEmpty())
                // The notice went to the store the batch ran under, not the one open by then.
                assertEquals(listOf<OpenAccount>(storeA), notices.into)
            },
            Row("d: the sync stopping under a command after the change") {
                clear(1)
                test.runCurrent()
                opened.value = storeB
                answer(listOf(Result.failure(IllegalStateException("The reading sync stopped"))))
                test.runCurrent()
                assertTrue(notices.stored.isEmpty())
                assertTrue(runner.summaries.value.isEmpty())
            },
            Row("e: AccountChanged") {
                answer(listOf(Result.failure(SyncUnavailable.AccountChanged())))
                clear(1)
                test.runCurrent()
                assertTrue(notices.stored.isEmpty())
                assertTrue(runner.summaries.value.isEmpty())
                assertEquals(1, signals.count { it is CatalogSignal.BatchEnded })
            },
            Row("f: teardown while saving") {
                notices.fails = { true }
                answer(listOf(refused()))
                clear(1)
                test.runCurrent()
                assertTrue(runner.state.value!!.saving)
                test.advanceTimeBy(1_000)
                test.runCurrent()
                opened.value = null
                runner.stop()
                val attempts = notices.attempts
                test.advanceTimeBy(60_000)
                test.runCurrent()
                assertEquals(attempts, notices.attempts)
                assertTrue(runner.summaries.value.isEmpty())
            },
            Row("g: no admission during teardown") {
                var during: Long? = -1
                clear(1)
                test.runCurrent()
                test.backgroundScope.launch {
                    runner.state.first { it == null }
                    during = clear(1, "a")
                }
                test.runCurrent()
                runner.stop()
                test.runCurrent()
                assertNull(during)
            },
            Row("h: the queue filter") {
                answer(listOf(applied))
                clear(1)
                test.runCurrent()
                assertEquals(1, runner.summaries.value.size)
                runner.stop()
                assertEquals(1, runner.summaries.value.size)
                opened.value = null
                runner.stop()
                assertTrue(runner.summaries.value.isEmpty())
            },
            Row("i: another store opens with a summary queued") {
                answer(listOf(applied))
                clear(1)
                test.runCurrent()
                val summary = runner.summaries.value.single()
                assertTrue(runner.pending(summary))
                opened.value = storeB
                assertFalse(runner.pending(summary))
                assertFalse(runner.pendingFlow(summary).first())
            },
        )
        for (row in rows) {
            runTest {
                val f = Fixture(this)
                f.(row.script)()
                runCurrent()
                assertNull(row.name, f.runner.state.value)
                f.opened.value = Store("a")
                assertNotNull(row.name, f.clear(1))
            }
        }
    }
}
