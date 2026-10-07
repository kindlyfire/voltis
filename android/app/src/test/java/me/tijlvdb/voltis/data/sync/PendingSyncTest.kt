package me.tijlvdb.voltis.data.sync

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.async
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.AccountChangedException
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.data.db.OpenAccount
import me.tijlvdb.voltis.data.reading.EngineTest
import me.tijlvdb.voltis.domain.reading.DrainResult
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.domain.sync.InMemoryPendingStore
import me.tijlvdb.voltis.domain.sync.NoticeKind
import me.tijlvdb.voltis.domain.sync.PendingChange
import me.tijlvdb.voltis.domain.sync.UserDataItem
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The pending star and rating rows (P4 §8): the run's answers table, the owner's rules, and what `PendingSync` binds. */
@OptIn(ExperimentalCoroutinesApi::class)
class PendingSyncTest {
    /** A transport answering from a script; each request waits for its gate, when one is set, before it answers. */
    class FakeTransport : PendingTransport {
        val sent = mutableListOf<Pair<String, JsonObject>>()
        var answer: suspend (contentId: String) -> UserData = { UserData(starred = true) }
        val gates = ArrayDeque<CompletableDeferred<Unit>>()

        override suspend fun updateUserData(contentId: String, body: JsonObject): UserData {
            sent += contentId to body
            gates.removeFirstOrNull()?.await()
            return answer(contentId)
        }
    }

    private val store = InMemoryPendingStore()
    private val transport = FakeTransport()
    private val connectivity = EngineTest.FakeConnectivity { true }
    private val scheduler = EngineTest.FakeScheduler()
    private val events = CatalogEvents()
    private val announced = mutableListOf<Long>()
    private val errors = mutableListOf<Throwable>()

    /** Started over [rows], which are empty then: a test stores its rows and asks for the one run it looks at. */
    private fun TestScope.owner(rows: InMemoryPendingStore = store, sent: PendingTransport = transport, scheduling: EngineTest.FakeScheduler = scheduler) =
        PendingOwner(
            "a", rows, sent, connectivity, scheduling, { announced += it.map { n -> n.id } }, events,
            dispatcher = StandardTestDispatcher(testScheduler), clock = { testScheduler.currentTime }, onError = { _, e -> errors += e },
        ).also {
            backgroundScope.launch { it.start() }
            runCurrent()
        }

    private fun item(id: String) = UserDataItem(id, "l", "u-$id", "Title $id")

    private suspend fun InMemoryPendingStore.want(id: String, starred: Boolean? = true, rating: Int? = null, now: Long = 1) =
        commit(PendingChange.Submit(item(id), starred, rating?.let { JsonPrimitive(it) }, now))

    private fun body(starred: Boolean? = null, rating: Int? = null) = JsonObject(
        buildMap {
            starred?.let { put("starred", JsonPrimitive(it)) }
            rating?.let { put("rating", JsonPrimitive(it)) }
        },
    )

    /** One run over one row for each answer of §8: the row, the notice, the run's end, the landing's event and what it asked for. */
    @Test
    fun answers() = runTest {
        class Case(val name: String, val failure: Exception?, val probe: Boolean = true, val full: Boolean = false, val row: String, val result: DrainResult, val scheduled: Boolean, val notice: String? = null)
        for (case in listOf(
            Case("2xx", null, row = "landed", result = DrainResult.DONE, scheduled = false),
            Case("refused", ReadingFailure.Refused(400, "invalid rating"), row = "none", result = DrainResult.DONE, scheduled = false, notice = NoticeKind.REFUSED),
            Case("gone", ReadingFailure.Gone("Content not found"), row = "none", result = DrainResult.DONE, scheduled = false, notice = NoticeKind.GONE),
            Case("5xx", ReadingFailure.ServerError(500, "boom"), row = "attempts 1", result = DrainResult.WAITING, scheduled = true),
            Case("unreachable, probe fails", ReadingFailure.Unreachable(), probe = false, row = "attempts 0", result = DrainResult.WAITING, scheduled = true),
            Case("unreachable, probe passes", ReadingFailure.Unreachable(), row = "attempts 1", result = DrainResult.WAITING, scheduled = true),
            Case("signed out", ReadingFailure.SignedOut(), row = "attempts 0", result = DrainResult.WAITING, scheduled = true),
            Case("account changed", AccountChangedException(), row = "attempts 0", result = DrainResult.WAITING, scheduled = true),
            Case("refused commit", null, full = true, row = "attempts 0", result = DrainResult.WAITING, scheduled = true),
        )) {
            val rows = InMemoryPendingStore()
            val sent = FakeTransport()
            val scheduling = EngineTest.FakeScheduler()
            val landings = mutableListOf<CatalogChange>()
            val watching = backgroundScope.launch { events.changes.collect { landings += it } }
            rows.fullFor = { case.full && it is PendingChange.Landed }
            connectivity.reachable = { case.probe }
            sent.answer = { case.failure?.let { throw it } ?: UserData(starred = true, rating = 4) }
            announced.clear()
            connectivity.online.value = true
            val owner = owner(rows, sent, scheduling)
            rows.want("c_1", true, 4)
            val result = owner.request(RunKind.FOREGROUND).await()
            runCurrent()

            val row = rows.rows.value["c_1"]
            val shown = when {
                row == null -> "none"
                row.landed != null && !row.wanted -> "landed"
                else -> "attempts ${row.attempts}"
            }
            assertEquals(case.name, listOf(case.row, case.result, case.scheduled, case.notice), listOf(shown, result, scheduling.scheduled > 0, rows.notices.singleOrNull()?.kind))
            assertEquals(case.name, if (case.failure == null && !case.full) 1 else 0, landings.size)
            assertEquals(case.name, if (case.notice == null) 0 else 1, announced.size)
            // The request carried the wanted values, the rating as a number.
            assertEquals(case.name, body(true, 4), sent.sent.first().second)
            // A 5xx and a passing probe keep the error; a probe that fails and the rest count nothing.
            assertEquals(case.name, case.row == "attempts 1", row?.lastError != null)
            owner.stop()
            watching.cancel()
        }
    }

    /** A row merged while its request is out survives the answer and goes again; a failure of the old request counts nothing against it. */
    @Test
    fun aMergeDuringARequestIsSentAgain() = runTest {
        val owner = owner()
        val first = CompletableDeferred<Unit>()
        transport.gates += first
        owner.submit(item("c_1"), true, null)
        runCurrent()
        // Merged while the first is out: the rating joins the star in one wish.
        owner.submit(item("c_1"), null, JsonPrimitive(4))
        transport.answer = { UserData(starred = true, rating = 4) }
        first.complete(Unit)
        runCurrent()
        assertEquals(listOf(body(true), body(true, 4)), transport.sent.map { it.second })
        assertEquals(emptyList<Any>(), store.wanted())

        // The old request fails at a moved rev: no attempt and no error, though its target is set aside for that run.
        val old = CompletableDeferred<Unit>()
        val newer = CompletableDeferred<Unit>()
        transport.gates += listOf(old, newer)
        transport.answer = { throw ReadingFailure.ServerError(500, "boom") }
        owner.submit(item("c_2"), true, null)
        runCurrent()
        owner.submit(item("c_2"), null, JsonPrimitive(2))
        old.complete(Unit)
        runCurrent()
        assertEquals(listOf(0, null), store.rows.value.getValue("c_2").let { listOf(it.attempts, it.lastError) })
        // The merged wish is the one sent next, and counts when it fails.
        newer.complete(Unit)
        runCurrent()
        assertEquals(listOf(1, "boom", body(true, 2)), store.rows.value.getValue("c_2").let { listOf(it.attempts, it.lastError, transport.sent.last().second) })
        owner.stop()
    }

    /** Three failed runs make a row stuck: only the one-time worker alone leaves it be; Retry and Discard take the mutex. */
    @Test
    fun stuckRows() = runTest {
        val owner = owner()
        transport.answer = { throw ReadingFailure.ServerError(500, "boom") }
        store.want("c_1", now = 1)
        store.want("c_2", now = 2)
        // One try per run and per row.
        repeat(3) { owner.request(RunKind.FOREGROUND).await() }
        assertEquals(listOf(3, 3), store.wanted().map { it.attempts })
        assertEquals(listOf("c_1", "c_2"), owner.stuck.first().map { it.contentId })

        // Only stuck rows are left: the one-time worker is done without a request. The periodic one, a foreground one and a coalesced pair try them.
        transport.sent.clear()
        assertEquals(DrainResult.DONE, owner.request(RunKind.ONE_TIME).await())
        assertEquals(0, transport.sent.size)
        assertEquals(DrainResult.DONE, owner.request(RunKind.PERIODIC).await())
        assertEquals(2, transport.sent.size)
        listOf(owner.request(RunKind.ONE_TIME), owner.request(RunKind.FOREGROUND)).forEach { it.await() }
        assertEquals(4, transport.sent.size)

        // Retry resets the attempts and sends; Discard clears the wish without a request.
        transport.answer = { if (it == "c_2") throw ReadingFailure.ServerError(500, "boom") else UserData(starred = true) }
        transport.sent.clear()
        owner.retry("c_1")
        runCurrent()
        assertEquals("c_1", transport.sent.first().first)
        owner.discard("c_2")
        transport.sent.clear()
        assertEquals(DrainResult.DONE, owner.request(RunKind.FOREGROUND).await())
        assertEquals(0, transport.sent.size)
        assertEquals(emptyList<Any>(), store.wanted())
        assertNull(store.rows.value["c_2"])

        // A stuck row that was there before a fresh owner opened: the startup run (the process may be a worker's) and a
        // reconnection leave it be, as does the one-time worker; a real foreground run sends it.
        store.want("c_3", now = 3)
        repeat(3) { store.commit(PendingChange.Failed("c_3", store.rows.value.getValue("c_3").rev, "boom")) }
        owner.stop()
        transport.sent.clear()
        val fresh = owner()
        assertEquals(0, transport.sent.size)
        connectivity.online.value = false
        runCurrent()
        connectivity.online.value = true
        runCurrent()
        assertEquals(DrainResult.DONE, fresh.request(RunKind.ONE_TIME).await())
        assertEquals(0, transport.sent.size)
        fresh.request(RunKind.FOREGROUND).await()
        assertEquals(listOf("c_3"), transport.sent.map { it.first })
        fresh.stop()
    }

    /** Offline a submit stores and schedules, and the one-time worker sends it whatever `online` says. */
    @Test
    fun offlineThenTheBackground() = runTest {
        connectivity.online.value = false
        val owner = owner()
        owner.submit(item("c_1"), true, null)
        runCurrent()
        assertEquals(listOf(0, true), listOf(transport.sent.size, scheduler.scheduled > 0))
        assertEquals(DrainResult.DONE, owner.request(RunKind.ONE_TIME).await())
        assertEquals(listOf("c_1"), transport.sent.map { it.first })
        assertEquals(emptyList<Any>(), store.wanted())
        owner.stop()
    }

    /** A refused submit changes nothing; a prune that fails doesn't keep the owner from starting; a stop leaves nothing running. */
    @Test
    fun faultsAndStop() = runTest {
        store.pruneFails = true
        store.fullFor = { it is PendingChange.Submit }
        val owner = owner()
        assertTrue(runCatching { owner.submit(item("c_1"), true, null) }.exceptionOrNull() is StorageFullException)
        assertEquals(emptyMap<String, Any>(), store.rows.value)
        // The prune failed and was reported, and the owner runs.
        assertEquals(1, errors.size)
        store.fullFor = { false }
        val gate = CompletableDeferred<Unit>()
        transport.gates += gate
        owner.submit(item("c_1"), true, null)
        runCurrent()
        val waiting = owner.request(RunKind.FOREGROUND)
        // Stopped with a request out and a waiter: the row is as it was, the waiter hears an account change, and nothing runs after.
        owner.stop()
        assertTrue(runCatching { waiting.await() }.exceptionOrNull() is SyncUnavailable.AccountChanged)
        gate.complete(Unit)
        runCurrent()
        assertEquals(listOf(true, 1L), store.rows.value.getValue("c_1").let { listOf(it.wanted, it.rev) })
        assertEquals(1, transport.sent.size)
        assertTrue(runCatching { owner.submit(item("c_1"), false, null) }.exceptionOrNull() is SyncUnavailable.AccountChanged)
        assertTrue(runCatching { owner.request(RunKind.FOREGROUND).await() }.exceptionOrNull() is SyncUnavailable.AccountChanged)

        // Stopped during a write: it is cancelled before it changed anything, or it finished whole; stop returns, and nothing is scheduled after.
        val held = InMemoryPendingStore().apply { commitGate = CompletableDeferred() }
        val later = EngineTest.FakeScheduler()
        val writing = owner(held, scheduling = later)
        val submitted = backgroundScope.async { runCatching { writing.submit(item("c_9"), true, null) }.exceptionOrNull() }
        runCurrent()
        val stopped = backgroundScope.launch { writing.stop() }
        runCurrent()
        held.commitGate!!.complete(Unit)
        runCurrent()
        assertTrue(stopped.isCompleted)
        assertTrue(submitted.await() is SyncUnavailable.AccountChanged)
        assertTrue(held.rows.value["c_9"].let { it == null || it.wanted && it.rev == 1L })
        assertEquals(0, later.scheduled)
        testScheduler.advanceUntilIdle()
        assertEquals(0, later.scheduled)
    }

    private class Open(override val account: String) : OpenAccount

    /** The binding: one owner for the open store, requests for another account refused, the background drain of the signed-in account's directory only. */
    @Test
    fun theBoundOwner() = runTest {
        val current = MutableStateFlow<OpenAccount?>(null)
        val closed = mutableListOf<String>()
        val sync = PendingSync(
            current, { "a" }, MutableStateFlow("a"), MutableStateFlow(null),
            newOwner = { open, fg -> PendingOwner(open.account, store, transport, connectivity, scheduler, {}, events, StandardTestDispatcher(testScheduler), { 0 }, foreground = fg) },
            closeWork = { closed += it },
            scope = backgroundScope,
        )
        current.value = Open("a")
        runCurrent()
        // The page's flow follows the owner of its own account only.
        assertNull(sync.pending("b", "c_1").first())
        sync.setUserData("a", item("c_1"), starred = true)
        runCurrent()
        assertEquals(listOf("c_1"), transport.sent.map { it.first })
        assertEquals(1L, sync.landedSeq("a"))
        assertTrue(runCatching { sync.setUserData("b", item("c_1"), starred = true) }.exceptionOrNull() is SyncUnavailable.AccountChanged)

        // No row to send: done without a request. Another account's directory: nothing.
        assertEquals(DrainResult.DONE, sync.drainInBackground(AccountStores.directoryName("a"), periodic = true))
        assertNull(sync.drainInBackground(AccountStores.directoryName("b"), periodic = false))
        assertEquals(1, transport.sent.size)

        // The store closes: the account's work is closed, and no owner is left.
        current.value = null
        sync.stop()
        assertEquals(listOf("a"), closed)
        assertEquals(0, sync.unsent.first())

        // A bind held in its prune while the store closes: stop waits for it, then clears and stops the owner it published.
        val held = InMemoryPendingStore().apply { pruneGate = CompletableDeferred() }
        val made = mutableListOf<PendingOwner>()
        val racing = PendingSync(
            current, { "a" }, MutableStateFlow("a"), MutableStateFlow(null),
            newOwner = { open, fg -> PendingOwner(open.account, held, transport, connectivity, scheduler, {}, events, StandardTestDispatcher(testScheduler), { 0 }, foreground = fg).also(made::add) },
            closeWork = { closed += it },
            scope = backgroundScope,
        )
        current.value = Open("a")
        runCurrent()
        current.value = null
        val stopping = backgroundScope.launch { racing.stop() }
        runCurrent()
        assertTrue(!stopping.isCompleted)
        held.pruneGate!!.complete(Unit)
        runCurrent()
        assertTrue(stopping.isCompleted)
        assertEquals(1, made.size)
        assertTrue(runCatching { made.single().request(RunKind.FOREGROUND).await() }.exceptionOrNull() is SyncUnavailable.AccountChanged)
        assertEquals(0, racing.unsent.first())
        assertNull(racing.pending("a", "c_1").first())

        // A foreground() before the owner is bound (its prune held): the stuck row left from before is sent once it is, while online.
        val stuck = InMemoryPendingStore().apply { pruneGate = CompletableDeferred() }
        stuck.want("c_5")
        repeat(3) { stuck.commit(PendingChange.Failed("c_5", stuck.rows.value.getValue("c_5").rev, "boom")) }
        val late = PendingSync(
            current, { "a" }, MutableStateFlow("a"), MutableStateFlow(null),
            newOwner = { open, fg -> PendingOwner(open.account, stuck, transport, connectivity, scheduler, {}, events, StandardTestDispatcher(testScheduler), { 0 }, foreground = fg) },
            closeWork = {},
            scope = backgroundScope,
        )
        transport.sent.clear()
        current.value = Open("a")
        runCurrent()
        late.foreground()
        stuck.pruneGate!!.complete(Unit)
        runCurrent()
        assertEquals(listOf("c_5"), transport.sent.map { it.first })
        late.background()
        current.value = null
        late.stop()

        // A background() while the bind is held in its prune, released offline: the owner reads the app's state live, so a reconnection
        // in the background leaves the stuck row be, and the next foreground() sends it.
        val gated = InMemoryPendingStore().apply { pruneGate = CompletableDeferred() }
        gated.want("c_6")
        repeat(3) { gated.commit(PendingChange.Failed("c_6", gated.rows.value.getValue("c_6").rev, "boom")) }
        val net = EngineTest.FakeConnectivity { true }
        net.online.value = false
        val flipped = PendingSync(
            current, { "a" }, MutableStateFlow("a"), MutableStateFlow(null),
            newOwner = { open, fg -> PendingOwner(open.account, gated, transport, net, scheduler, {}, events, StandardTestDispatcher(testScheduler), { 0 }, foreground = fg) },
            closeWork = {},
            scope = backgroundScope,
        )
        transport.sent.clear()
        flipped.foreground()
        current.value = Open("a")
        runCurrent()
        flipped.background()
        gated.pruneGate!!.complete(Unit)
        runCurrent()
        net.online.value = true
        runCurrent()
        assertEquals(emptyList<String>(), transport.sent.map { it.first })
        flipped.foreground()
        runCurrent()
        assertEquals(listOf("c_6"), transport.sent.map { it.first })
        current.value = null
        flipped.stop()
    }
}
