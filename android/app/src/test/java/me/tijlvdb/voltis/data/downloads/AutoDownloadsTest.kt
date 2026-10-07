package me.tijlvdb.voltis.data.downloads

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.atomic.AtomicInteger
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.async
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.data.db.LaneEntity
import me.tijlvdb.voltis.data.db.SeriesPolicyEntity
import me.tijlvdb.voltis.domain.downloads.DownloadError
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.db.importReading
import me.tijlvdb.voltis.domain.reading.ReadingSnapshot
import me.tijlvdb.voltis.domain.reading.ReadingState
import me.tijlvdb.voltis.data.reading.ReadingHolds
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.RequestedBy
import me.tijlvdb.voltis.domain.downloads.SeriesPolicy
import me.tijlvdb.voltis.domain.storage.StorageFullException
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** The triggers of automatic downloads (P2 §17), on a real owner and database, with the reconcile and the scheduling counted. Real time, a short retry. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [37], application = Application::class)
class AutoDownloadsTest {
    @get:Rule
    val temp = TemporaryFolder()

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val db = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), VoltisDatabase::class.java).build()
    private lateinit var store: DownloadStore
    private val dir get() = temp.root.name

    private val reconciles = AtomicInteger()
    private val schedules = CopyOnWriteArrayList<Boolean>()
    @Volatile private var failReconcile = false
    @Volatile private var failSchedule = false
    @Volatile private var current: DownloadStore? = null
    @Volatile private var scheduleGate: Gate? = null
    @Volatile private var currentGate: Gate? = null
    @Volatile private var committedGate: Gate? = null

    /** Holds a caller at a point until the test lets it go. */
    private class Gate {
        val entered = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
    }
    private val owners = MutableStateFlow<DownloadStore?>(null)

    private val auto by lazy {
        AutoDownloads(
            {
                val now = current
                // The caller has its answer; it stops before acting on it.
                currentGate?.let { gate ->
                    currentGate = null
                    gate.entered.complete(Unit)
                    runBlocking { gate.release.await() }
                }
                now
            },
            owners,
            reconcile = { it, series ->
                reconciles.incrementAndGet()
                if (failReconcile) throw StorageFullException()
                it.reconcileAuto(series)
            },
            schedule = { owner, replace, wake ->
                scheduleGate?.let { gate ->
                    scheduleGate = null
                    gate.entered.complete(Unit)
                    gate.release.await()
                }
                if (failSchedule) throw QueueSubmitFailed(IllegalStateException("disk"))
                schedules += replace
                wake?.let(owner::service)
            },
            clock = System::currentTimeMillis,
            retryAfter = RETRY,
            sampleMs = 50,
            ownerWait = 300,
        )
    }

    @After
    fun close() = runBlocking {
        ReadingHolds.set(dir, false)
        store.stop()
        scope.cancel()
        db.close()
    }

    private suspend fun start() {
        store = DownloadStore(
            AccountStore("a", temp.root, db), scope, { store.collectSoon(it.dirId) },
            committed = {
                committedGate?.let { gate ->
                    committedGate = null
                    gate.entered.complete(Unit)
                    gate.release.await()
                }
            },
        )
        current = store
        owners.value = store
        db.cache(listOf(Content("lantern", "Lantern", ContentType.COMIC_SERIES) to null), System.currentTimeMillis()) { true }
        db.cache((1..5).map { Content("v$it", "Vol $it", ContentType.COMIC, parentId = "lantern") to it - 1 }, System.currentTimeMillis()) { false }
    }

    private var seq = 0L

    /** The lanes, and the server states they acknowledged: what the rule reads. */
    private suspend fun VoltisDatabase.seen(vararg lanes: LaneEntity) {
        lanes().upsert(lanes.toList())
        importReading(lanes.map { ReadingSnapshot(it.contentId, ReadingState(status = it.status, seq = ++seq)) })
    }

    private fun lane(id: String, status: String) = LaneEntity(
        id, null, null, "lantern", ContentType.COMIC, id, null, true, false, null, status, null, "{}", null, null, 0, 0, null, null,
        true, false, 0, false, status, "{}", null, 0,
    )

    private suspend fun finished(vararg ids: String) {
        db.downloads().insert(ids.map { DownloadEntity(it, "lantern", DownloadState.DONE, requestedBy = RequestedBy.AUTO, queuedAt = 1, copyId = "c-$it", copyVersion = "v", copyPageCount = 2, copyBytes = 8) })
    }

    private fun follow(): Job = scope.launch { auto.follow(store) }

    private suspend fun eventually(what: String, check: suspend () -> Boolean) {
        withTimeout(5_000) { while (!check()) delay(20) }
    }

    private suspend fun quiet() = delay(RETRY * 2)

    private suspend fun rows() = db.downloads().all().map { it.contentId }.toSet()

    @Test
    fun followingReconcilesOnlyWhenThereIsSomethingToDo() = runBlocking {
        start()
        finished("v1")
        store.setPolicy("lantern", SeriesPolicy("lantern", 2, true))
        val job = follow()
        // The policy's own queueing was a wake: scheduled once. Then a steady state: lane commits that move nothing make no command.
        eventually("woken") { schedules.isNotEmpty() }
        schedules.clear()
        quiet()
        repeat(3) { db.seen(lane("v1", "reading")) }
        quiet()
        assertEquals(0, reconciles.get())

        // A completed volume acknowledged with nothing unsent: one reconcile queues the window and deletes it.
        db.seen(lane("v1", "completed"))
        eventually("v1 deleted") { "v1" !in rows() }
        assertEquals(setOf("v2", "v3"), rows())
        eventually("scheduled") { schedules.isNotEmpty() }
        assertEquals(listOf(false), schedules.toList())
        quiet()
        assertEquals(1, reconciles.get())
        job.cancelAndJoin()
    }

    @Test
    fun aCommittedQueueChangeSchedulesAndPageCommitsDoNot() = runBlocking {
        start()
        val job = follow()
        quiet()
        assertTrue(schedules.isEmpty())

        // A queue change commits: the owner's follower schedules it.
        store.enqueue(listOf(NewDownload("v1", "lantern", RequestedBy.USER)))
        eventually("scheduled") { schedules.isNotEmpty() }
        assertEquals(listOf(false), schedules.toList())

        // A running transfer's commits are no wake, and no plan is worked out for each.
        store.runNext { run ->
            store.manifest(run, OfflineManifest(1, "v1", "v", fileSize = 1, fileMtime = "m", pageCount = 3, from = 0, pages = emptyList()), 1)
            repeat(3) { store.page(run, it, 1) }
        }
        quiet()
        // The run ended without an end report: requeued, which is no control's wake either.
        assertEquals(1, schedules.size)
        job.cancelAndJoin()
    }

    /** A caller cancelled right after its command committed: the follower submits the wake, once, though the plan is empty afterwards. */
    @Test
    fun aCancelledCallerAfterItsCommitStillSchedulesOnce() = runBlocking {
        start()
        suspend fun cancelledAfterCommit(command: suspend () -> Unit) {
            val gate = Gate().also { committedGate = it }
            val caller = scope.launch { command() }
            gate.entered.await()
            caller.cancel()
            gate.release.complete(Unit)
            caller.join()
        }

        // reconcileAuto queues the window; nobody submits it.
        db.auto().upsertPolicy(SeriesPolicyEntity("lantern", 2, false, 0))
        cancelledAfterCommit { store.reconcileAuto() }
        assertEquals(setOf("v1", "v2"), rows())
        val job = follow()
        eventually("scheduled") { schedules.isNotEmpty() }
        quiet()
        assertEquals(listOf(false), schedules.toList())

        // Retry of a row that stays queued: its backoff must be cut short.
        schedules.clear()
        db.downloads().update(db.downloads().get("v1")!!.copy(error = DownloadError.UNREACHABLE, attempts = 2))
        cancelledAfterCommit { store.retry("v1") }
        assertEquals(null, db.downloads().get("v1")!!.error)
        eventually("scheduled again") { schedules.isNotEmpty() }
        quiet()
        assertEquals(listOf(false), schedules.toList())
        job.cancelAndJoin()
    }

    @Test
    fun theHoldsReleaseIsATriggerAndAnUnpinnedCopyWasNeverLost() = runBlocking {
        start()
        store.setPolicy("lantern", SeriesPolicy("lantern", 0, true))
        finished("v1", "v2")
        db.seen(lane("v1", "completed"), lane("v2", "completed"))
        val pin = store.openCopy("v2", Job())!!
        ReadingHolds.set(dir, true)
        // Nothing can be deleted yet: the plan is empty, and so is the command.
        val job = follow()
        quiet()
        assertEquals(0, reconciles.get())

        // The engine's hold goes with no write at all: the pinned copy stays.
        ReadingHolds.set(dir, false)
        eventually("v1 deleted") { rows() == setOf("v2") }
        // The pin's release alone, with the invalidation long consumed, deletes the other.
        pin.release()
        eventually("v2 deleted") { rows().isEmpty() }
        job.cancelAndJoin()
    }

    @Test
    fun aRefusedReconcileOrSubmissionKeepsFollowingAndTheRetryMakesItUp() = runBlocking {
        start()
        store.setPolicy("lantern", SeriesPolicy("lantern", 0, true))
        finished("v1", "v2")
        val job = follow()
        quiet()

        // A refused commit: followed on, and the retry applies it. Its request schedules too.
        failReconcile = true
        db.seen(lane("v1", "completed"))
        eventually("refused") { reconciles.get() >= 1 }
        assertEquals(setOf("v1", "v2"), rows())
        failReconcile = false
        eventually("retried") { rows() == setOf("v2") }
        eventually("scheduled") { schedules.isNotEmpty() }
        assertTrue(job.isActive)

        // A failed submission with a plain cause, after rows were queued: the rows stay, and with an empty plan the retry schedules them.
        schedules.clear()
        store.setPolicy("lantern", SeriesPolicy("lantern", 2, true))
        failSchedule = true
        db.seen(lane("v2", "completed"))
        eventually("queued") { "v3" in rows() }
        quiet()
        assertTrue(schedules.isEmpty())
        val before = reconciles.get()
        failSchedule = false
        eventually("made up") { schedules.isNotEmpty() }
        // Nothing changed in the database meanwhile: the plan was empty, and no command was made for it.
        assertEquals(before, reconciles.get())
        assertTrue(job.isActive)
        job.cancelAndJoin()
    }

    @Test
    fun theRetryBelongsToTheCurrentOwnerAndWaitsForItsFollow() = runBlocking {
        start()
        // Armed before follow registered, with the replace flag: kept, and run once after the delay.
        auto.retrySoon(store, replace = true)
        val job = follow()
        eventually("scheduled") { schedules.isNotEmpty() }
        assertEquals(listOf(true), schedules.toList())
        quiet()
        assertEquals(1, schedules.size)

        // A stale owner is rejected; an owner change clears what was armed.
        val stale = DownloadStore(AccountStore("b", temp.newFolder("b"), db), scope, {})
        auto.retrySoon(stale, replace = false)
        auto.retrySoon(store, replace = false)
        current = stale
        auto.ownerChanged()
        quiet()
        assertEquals(1, schedules.size)
        stale.stop()
        job.cancelAndJoin()
    }

    @Test
    fun aRetryUpgradedWhileItRunsIsArmedAgain() = runBlocking {
        start()
        auto.retrySoon(store, replace = false)
        val gate = Gate().also { scheduleGate = it }
        val job = follow()
        // The retry fired and is scheduling with replace = false; the upgrade arrives meanwhile and isn't part of that.
        gate.entered.await()
        auto.retrySoon(store, replace = true)
        gate.release.complete(Unit)
        // No database or settings emission follows: only a fresh timer can make it up.
        eventually("replaced") { schedules.toList() == listOf(false, true) }
        quiet()
        assertEquals(2, schedules.size)
        job.cancelAndJoin()
    }

    @Test
    fun aStaleOwnersArmCannotOverwriteTheCurrentOwnersRetry() = runBlocking {
        start()
        val b = DownloadStore(AccountStore("b", temp.newFolder("b"), db), scope, {})
        // A passes its ownership check, then the account switches and B registers its failure.
        val gate = Gate().also { currentGate = it }
        val armA = scope.async { auto.retrySoon(store, replace = false) }
        gate.entered.await()
        // B is current and has registered before the owner-change callback runs; A's stale registration comes after.
        current = b
        auto.retrySoon(b, replace = true)
        gate.release.complete(Unit)
        armA.await()
        // A late callback may not clear B's request.
        auto.ownerChanged()

        val job = scope.launch { auto.follow(b) }
        eventually("B scheduled") { schedules.isNotEmpty() }
        assertEquals(listOf(true), schedules.toList())
        job.cancelAndJoin()
        b.stop()
    }

    @Test
    fun anOwnerChangeDuringTheCurrentOwnersArmKeepsItsRetry() = runBlocking {
        start()
        // B is current; its first registration stops after the ownership check, the callback runs, then B goes on.
        val gate = Gate().also { currentGate = it }
        val armB = scope.async { auto.retrySoon(store, replace = true) }
        gate.entered.await()
        auto.ownerChanged()
        gate.release.complete(Unit)
        armB.await()

        val job = follow()
        eventually("scheduled") { schedules.isNotEmpty() }
        assertEquals(listOf(true), schedules.toList())
        job.cancelAndJoin()
    }

    @Test
    fun settleReconcilesSchedulesAndAsksForARetryOnFailure() = runBlocking {
        start()
        store.setPolicy("lantern", SeriesPolicy("lantern", 0, false))
        // Another account's directory: nothing happens, and at once.
        val began = System.nanoTime()
        assertTrue(auto.settle("other"))
        assertTrue(System.nanoTime() - began < 250_000_000)
        assertEquals(0, reconciles.get())

        assertTrue(auto.settle(dir))
        assertEquals(1, reconciles.get())
        assertEquals(listOf(false), schedules.toList())

        failReconcile = true
        assertFalse(auto.settle(dir))
        failReconcile = false
        failSchedule = true
        assertFalse(auto.settle(dir))
        failSchedule = false
        assertTrue(auto.settle(dir))

        // A stopped owner is done, not a retry.
        store.stop()
        assertTrue(auto.settle(dir))
    }

    private companion object {
        const val RETRY = 100L
    }
}
