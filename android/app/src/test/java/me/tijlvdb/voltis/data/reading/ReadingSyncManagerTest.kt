package me.tijlvdb.voltis.data.reading

import kotlin.coroutines.CoroutineContext
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.data.db.OpenAccount
import me.tijlvdb.voltis.data.sync.PendingOwner
import me.tijlvdb.voltis.data.sync.PendingSync
import me.tijlvdb.voltis.data.sync.PendingSyncTest
import me.tijlvdb.voltis.data.sync.SyncCenter
import me.tijlvdb.voltis.domain.reading.AttentionItem
import me.tijlvdb.voltis.domain.reading.Change
import me.tijlvdb.voltis.domain.reading.ComicAdapter
import me.tijlvdb.voltis.domain.reading.DrainResult
import me.tijlvdb.voltis.domain.reading.FakeReadingServer
import me.tijlvdb.voltis.domain.reading.InMemoryReadingStore
import me.tijlvdb.voltis.domain.reading.Lane
import me.tijlvdb.voltis.domain.reading.Op
import me.tijlvdb.voltis.domain.reading.OpKind
import me.tijlvdb.voltis.domain.reading.ReviewOutcome
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.reading.position
import me.tijlvdb.voltis.domain.sync.InMemoryPendingStore
import me.tijlvdb.voltis.domain.sync.NoticeDetail
import me.tijlvdb.voltis.domain.sync.NoticeKind
import me.tijlvdb.voltis.domain.sync.PendingChange
import me.tijlvdb.voltis.domain.sync.StoredNotice
import me.tijlvdb.voltis.domain.sync.SyncNotices
import me.tijlvdb.voltis.domain.sync.UserDataItem
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** A reader that attaches before its account's engine runs: the stand-in session of [ReadingSyncManager]. */
@OptIn(ExperimentalCoroutinesApi::class)
class ReadingSyncManagerTest {
    private val opened = MutableStateFlow<String?>(null)
    /** The session's account, as `SessionStore.state.value` gives it. */
    private var account: String? = "a"

    /** Its change notifications, which a test can hold back. */
    private val changes = MutableStateFlow<String?>("a")
    private val failed = MutableStateFlow<String?>(null)
    private val stores = mutableMapOf<String, InMemoryReadingStore>()
    private val server = FakeReadingServer(9)
    private val adapter = ComicAdapter(pages = { 10 })
    private val connectivity = EngineTest.FakeConnectivity { true }
    private val notices = FakeNotices()
    private val faults = mutableMapOf<String, EngineTest.FaultyStore>()
    private val errors = mutableListOf<Throwable>()
    /** Lets a store fault be recorded, not thrown. */
    private var tolerate = false
    /** Runs on each new account's stores, before its engine starts. */
    private var prepare: (InMemoryReadingStore, EngineTest.FaultyStore) -> Unit = { _, _ -> }

    private fun signIn(to: String?) {
        account = to
        changes.value = to
    }

    private fun TestScope.manager(main: CoroutineDispatcher = StandardTestDispatcher(testScheduler), work: SyncWork? = null): ReadingSyncManager {
        return ReadingSyncManager(
            opened, { account }, changes, failed,
            newEngine = { account ->
                val inner = InMemoryReadingStore().also { stores[account] = it }
                val store = EngineTest.FaultyStore(inner).also { faults[account] = it }
                prepare(inner, store)
                ReadingEngine(
                    store, server, EngineTest.FakeWriter(), CatalogEvents(), connectivity,
                    work?.open(AccountStores.directoryName(account)) ?: EngineTest.FakeScheduler(), backgroundScope, main,
                    onError = { if (tolerate) errors += it else throw it },
                )
            },
            loadIdentity = {},
            connectivity = connectivity,
            notices = notices,
            scope = backgroundScope,
            main = main,
            closeWork = { work?.close(AccountStores.directoryName(it)) },
        )
    }

    private suspend fun failure(block: suspend () -> Unit) = try {
        block()
        null
    } catch (e: Exception) {
        e
    }

    @Test
    fun aStoreThatFailsToOpenEndsTheNoticesAndFailsTheLoad() = runTest {
        val sync = manager().attach("c_1", adapter)
        val notices = backgroundScope.async { sync.notices.toList() }
        failed.value = "a"
        runCurrent()
        // The notices end without an error, which would crash the screen collecting them.
        assertEquals(emptyList<Any>(), notices.await())
        assertTrue(failure { sync.load() } is SyncUnavailable.OfflineData)
    }

    @Test
    fun aPendingReaderIsNeverHandedToAnotherAccount() = runTest {
        val manager = manager()
        val sync = manager.attach("c_1", adapter)
        sync.moved(position(3, 10))
        signIn("b")
        opened.value = "b"
        runCurrent()
        assertTrue(failure { sync.load() } is SyncUnavailable.AccountChanged)
        assertNull(stores.getValue("b").lane("c_1"))
        // Calls bound to the old account reach nothing of the new one.
        val row = Content("c_2", "Lantern Isle Vol. 2", ContentType.COMIC)
        assertTrue(failure { manager.seed("a", listOf(row)) } is SyncUnavailable.AccountChanged)
        assertTrue(failure { manager.pageCount("a", "c_2", 12) } is SyncUnavailable.AccountChanged)
        manager.seed("b", listOf(row))
        assertEquals(emptyMap<String, Any>(), manager.shown(setOf("c_2"), "a").first())
        assertEquals(setOf("c_2"), manager.shown(setOf("c_2"), "b").first().keys)
        // A page's command goes to the engine of the account signed in when it is made.
        manager.setStatus(row, "on_hold")
        assertEquals("on_hold", stores.getValue("b").lane("c_2")?.state?.status)

        // A reader of the new account attaches to its engine.
        manager.attach("c_1", adapter).load()
        assertNotNull(stores.getValue("b").lane("c_1"))

        // Needs attention: the account's held lanes with its notices; one item counts once.
        stores.getValue("b").commit(Change(lanes = listOf(Lane("c_3", title = "Lantern Isle Vol. 3", needsReview = true))))
        notices.stored.value = listOf(notice(1, "c_3"), notice(2, null))
        stores.getValue("b").commit(Change(notices = listOf(notice(0, "c_3"), notice(0, null))))
        assertEquals(listOf("c_3", "c_3", null), manager.attention("b").first().map { (it as? AttentionItem.Held)?.contentId ?: (it as AttentionItem.Notice).contentId })
        assertEquals(2, manager.attentionCount("b").first())
        assertEquals(emptyList<AttentionItem>(), manager.attention("a").first())
        assertTrue(failure { manager.dismiss("a", 1) } is SyncUnavailable.AccountChanged)
        assertTrue(failure { manager.review("a", "c_3") } is SyncUnavailable.AccountChanged)
        // The delete runs in the engine's own store.
        manager.dismiss("b", 1)
        assertEquals(listOf(2L), stores.getValue("b").notices.map { it.id })
    }

    @Test
    fun aReviewWhoseAdmissionFailsEndsFailed() = runTest {
        val manager = manager()
        tolerate = true
        opened.value = "a"
        runCurrent()
        faults.getValue("a").failLane = { true }
        val review = manager.review("a", "c_1")
        runCurrent()
        assertEquals(ReviewOutcome.FAILED, review.outcome.value)
        assertTrue(review.ended.value)
    }

    private fun notice(id: Long, contentId: String?) = StoredNotice(id, contentId, "Lantern Isle", NoticeKind.GONE, NoticeDetail(), 0)

    @Test
    fun aDetachedReaderIsNeverAttached() = runTest {
        val manager = manager()
        val gone = manager.attach("c_1", adapter)
        val waiting = manager.attach("c_2", adapter)
        gone.moved(position(3, 10))
        gone.detach()
        waiting.setVisible(true)
        opened.value = "a"
        runCurrent()
        assertTrue(failure { gone.load() } is CancellationException)
        assertNull(stores.getValue("a").lane("c_1"))

        // The other one attached, with what it was told meanwhile.
        waiting.load()
        waiting.moved(position(4, 10))
        testScheduler.advanceTimeBy(ReadingEngine.WRITE_DEBOUNCE + 1)
        runCurrent()
        assertEquals(position(4, 10), server.stateOf("c_2").progress)
        assertNull(server.rows["c_1"])
    }

    @Test
    fun theSessionDecidesNotALaggingNotification() = runTest {
        val manager = manager()
        changes.value = null
        // Restoring into the reader: the session is signed in, its notification hasn't come yet.
        val early = manager.attach("c_1", adapter)
        opened.value = "a"
        runCurrent()
        early.load()
        manager.attach("c_2", adapter).load()
        assertNotNull(stores.getValue("a").lane("c_1"))
        assertNotNull(stores.getValue("a").lane("c_2"))
    }

    @Test
    fun aSwitchBetweenTheEngineAndTheAttachmentAttachesNothing() = runTest {
        val main = Gate()
        val sync = manager(main).attach("c_1", adapter)
        main.run()
        // The engine starts, and the reader's account is replaced before it gets to attach.
        opened.value = "a"
        runCurrent()
        account = "b"
        main.run()
        runCurrent()
        assertTrue(failure { sync.load() } is SyncUnavailable.AccountChanged)
        assertNull(stores.getValue("a").lane("c_1"))
    }

    @Test
    fun theBackgroundDrainIsTheOpenAccounts() = runTest {
        val queue = FakeQueue()
        val work = SyncWork(queue::enqueue, queue::cancel)
        val manager = manager(work = work)
        val dirA = AccountStores.directoryName("a")
        val dirB = AccountStores.directoryName("b")
        // Work of a process before this one waits for A: nothing cancels it, at start or when A's engine opens.
        queue.active += SyncWork.workName(dirA)
        opened.value = "a"
        runCurrent()

        // Unreachable: the op stays, and a drain is asked for once WorkManager can take it.
        server.unreachable = true
        connectivity.reachable = { false }
        // The periodic safety net with an empty outbox does no drain.
        assertEquals(DrainResult.DONE, manager.drainInBackground(dirA, onlyIfUnsent = true))
        manager.setStatus(Content("c_1", "Lantern Isle Vol. 1", ContentType.COMIC), "on_hold")
        assertEquals(emptyList<String>(), queue.enqueued)
        work.begin()
        // The persisted one-time work keeps its place (KEEP drops the ask); the periodic one is enqueued.
        assertEquals(listOf(SyncWork.periodicName(dirA)), queue.enqueued)
        assertEquals(emptyList<String>(), queue.cancelled)
        runCurrent()
        assertEquals(DrainResult.WAITING, manager.drainInBackground(dirA))
        assertEquals(DrainResult.WAITING, manager.drainInBackground(dirA, onlyIfUnsent = true))

        // Another account's work drains nothing.
        assertNull(manager.drainInBackground(AccountStores.directoryName("b")))

        // Back: the worker's drain sends it, without `online` being refreshed.
        server.unreachable = false
        connectivity.reachable = { true }
        assertEquals(DrainResult.DONE, manager.drainInBackground(dirA))
        assertEquals("on_hold", server.stateOf("c_1").status)

        // The account's store closes: both its works are cancelled before its engine stops, and only those.
        signIn("b")
        manager.stop()
        assertEquals(listOf(SyncWork.workName(dirA), SyncWork.periodicName(dirA)), queue.cancelled)
        assertNull(manager.drainInBackground(dirA))
        // B's works are its own.
        work.open(dirB).schedule()
        assertEquals(listOf(SyncWork.periodicName(dirA), SyncWork.periodicName(dirB), SyncWork.workName(dirB)), queue.enqueued)
        assertEquals(setOf(SyncWork.periodicName(dirB), SyncWork.workName(dirB)), queue.active)
    }

    /** Through `SyncCenter`: the reading outbox and the pending stars of the account go together, and a stuck star only to the periodic work (P4 §8). */
    @Test
    fun theCenterDrainsBothInTheBackground() = runTest {
        val manager = manager()
        val rows = InMemoryPendingStore()
        val sent = PendingSyncTest.FakeTransport()
        val pendingStore = MutableStateFlow<OpenAccount?>(null)
        val pending = PendingSync(
            pendingStore, { account }, changes, failed,
            newOwner = { open, fg -> PendingOwner(open.account, rows, sent, connectivity, EngineTest.FakeScheduler(), {}, CatalogEvents(), StandardTestDispatcher(testScheduler), { 0 }, foreground = fg) },
            closeWork = {},
            scope = backgroundScope,
        )
        val center = SyncCenter(manager, pending)
        val dir = AccountStores.directoryName("a")
        opened.value = "a"
        pendingStore.value = object : OpenAccount { override val account = "a" }
        runCurrent()
        suspend fun star(id: String, attempts: Int = 0) =
            rows.commit(PendingChange.Submit(UserDataItem(id, "l", id, id), true, null, 1)).also { rows.rows.value = rows.rows.value + (id to rows.rows.value.getValue(id).copy(attempts = attempts)) }

        // Nothing unsent: the periodic work asks for nothing, and another account's directory drains neither.
        assertEquals(DrainResult.DONE, center.drainInBackground(dir, periodic = true))
        assertNull(center.drainInBackground(AccountStores.directoryName("b"), periodic = false))

        // A reading op that can't go yet, and a star: the star goes, and the answer is the op's.
        server.unreachable = true
        connectivity.reachable = { false }
        manager.setStatus(Content("c_1", "Lantern Isle Vol. 1", ContentType.COMIC), "on_hold")
        star("c_2")
        assertEquals(DrainResult.WAITING, center.drainInBackground(dir, periodic = false))
        assertEquals(listOf("c_2"), sent.sent.map { it.first })
        assertEquals(1, center.unsent.first())

        // Stuck, it is left to the periodic work, even while the one-time work retries for the op.
        sent.sent.clear()
        star("c_3", attempts = 3)
        assertEquals(DrainResult.WAITING, center.drainInBackground(dir, periodic = false))
        assertEquals(emptyList<String>(), sent.sent.map { it.first })
        assertEquals(DrainResult.WAITING, center.drainInBackground(dir, periodic = true))
        assertEquals(listOf("c_3"), sent.sent.map { it.first })

        // The server back: the op goes with the next drain.
        server.unreachable = false
        connectivity.reachable = { true }
        assertEquals(DrainResult.DONE, center.drainInBackground(dir, periodic = false))
        assertEquals("on_hold", server.stateOf("c_1").status)
    }

    /** The unsent count is unknown until both owners are one account's, and again when either changes. */
    @Test
    fun theCentersUnsentCountIsUnknownUntilBothOwnersAreOneAccounts() = runTest {
        val manager = manager()
        val pendingStore = MutableStateFlow<OpenAccount?>(null)
        val pending = PendingSync(
            pendingStore, { account }, changes, failed,
            newOwner = { open, fg -> PendingOwner(open.account, InMemoryPendingStore(), PendingSyncTest.FakeTransport(), connectivity, EngineTest.FakeScheduler(), {}, CatalogEvents(), StandardTestDispatcher(testScheduler), { 0 }, foreground = fg) },
            closeWork = {},
            scope = backgroundScope,
        )
        val seen = mutableListOf<Pair<String, Int>?>()
        backgroundScope.launch { SyncCenter(manager, pending).unsentOrNull.collect { seen += it?.let { read -> read.account to read.value } } }
        runCurrent()
        assertNull(seen.last())

        opened.value = "a"
        pendingStore.value = object : OpenAccount { override val account = "a" }
        runCurrent()
        assertEquals("a" to 0, seen.last())

        // The pending owner is another account's: nothing is known, rather than a mix.
        pending.stop()
        pendingStore.value = object : OpenAccount { override val account = "b" }
        runCurrent()
        assertNull(seen.last())
    }

    @Test
    fun backgroundDrainsRetryAStartThatFailed() = runTest {
        tolerate = true
        prepare = { inner, faulty ->
            runBlocking { inner.commit(Change(lanes = listOf(Lane("c_1")), ops = listOf(Op(contentId = "c_1", kind = OpKind.POSITION, payload = EngineTest.page(3))))) }
            faulty.failLoad = true
        }
        val manager = manager()
        opened.value = "a"
        runCurrent()
        assertEquals(1, errors.size)
        // The worker's drain, with the load failing still: retried later, not reported done.
        val dir = AccountStores.directoryName("a")
        assertEquals(DrainResult.WAITING, manager.drainInBackground(dir))
        assertEquals(emptyList<Any>(), server.sent)
        // Storage recovers: the next background drain restarts the engine first, with no foreground call.
        faults.getValue("a").failLoad = false
        assertEquals(DrainResult.DONE, manager.drainInBackground(dir))
        assertEquals(EngineTest.page(3), server.stateOf("c_1").progress)
    }

    /** WorkManager's unique work: KEEP drops an enqueue while work of that name is there. */
    private class FakeQueue {
        val active = mutableSetOf<String>()
        val enqueued = mutableListOf<String>()
        val cancelled = mutableListOf<String>()

        fun enqueue(name: String, dir: String, periodic: Boolean) {
            if (active.add(name)) enqueued += name
        }

        fun cancel(name: String) {
            cancelled += name
            active -= name
        }
    }

    /** A main thread that runs only when told. */
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

private class FakeNotices : SyncNotices {
    val stored = MutableStateFlow(emptyList<StoredNotice>())

    override suspend fun insert(store: OpenAccount, notice: StoredNotice, announce: Boolean) {
        stored.value += notice
    }

    override fun observe(): Flow<List<StoredNotice>> = stored

    override val fresh: SharedFlow<StoredNotice> = MutableSharedFlow()
}
