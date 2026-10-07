package me.tijlvdb.voltis.data.downloads

import android.app.Application
import android.database.sqlite.SQLiteFullException
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import kotlin.coroutines.CoroutineContext
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.cancel
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.job
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.data.db.LaneEntity
import me.tijlvdb.voltis.data.reading.ReadingHolds
import me.tijlvdb.voltis.domain.downloads.SeriesPolicy
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.db.importReading
import me.tijlvdb.voltis.domain.reading.ReadingSnapshot
import me.tijlvdb.voltis.domain.reading.ReadingState
import me.tijlvdb.voltis.domain.downloads.DownloadError
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.ErrorKind
import me.tijlvdb.voltis.domain.downloads.RequestedBy
import me.tijlvdb.voltis.domain.downloads.ServerFile
import me.tijlvdb.voltis.domain.downloads.Stale
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.storage.StorageFullException
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** The owner of an account's downloads: in-memory Room, a temporary directory, producers gated by deferreds and latches. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [37], application = Application::class)
class DownloadStoreTest {
    @get:Rule
    val temp = TemporaryFolder()

    private val scope = CoroutineScope(SupervisorJob())
    private val dbs = mutableListOf<VoltisDatabase>()
    private val stores = mutableListOf<DownloadStore>()
    private var ids = 0

    /** New transfer directories can't be named: a full disk in the middle of a command. */
    @Volatile private var failId = false

    /** Runs after a control's transaction, before its revocations. */
    @Volatile private var committed: (suspend () -> Unit)? = null

    /** Runs inside an automatic command's transaction, after its steps. */
    @Volatile private var committing: (suspend () -> Unit)? = null

    /** Runs in `finish`'s transaction. */
    @Volatile private var finishing: (suspend () -> Unit)? = null

    /** Runs in the start's recovery transaction. */
    @Volatile private var recovering: (suspend () -> Unit)? = null

    /** Told of each wipe, which then waits for [wipeGate]. */
    @Volatile private var wiping: CompletableDeferred<File>? = null

    @Volatile private var wipeGate: CountDownLatch? = null

    @After
    fun close() = runBlocking {
        stores.forEach { it.stop() }
        scope.cancel()
        dbs.forEach { it.close() }
    }

    private fun db() = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), VoltisDatabase::class.java).build().also { dbs += it }

    private fun newStore(
        dir: File = temp.root,
        db: VoltisDatabase = db(),
        io: CoroutineDispatcher = Dispatchers.IO,
        newId: () -> String = { if (failId) throw SQLiteFullException("database or disk is full") else "d${++ids}" },
    ): DownloadStore {
        lateinit var store: DownloadStore
        store = DownloadStore(
            AccountStore("a", dir, db), scope, { store.collectSoon(it.dirId) }, newId = newId, io = io,
            committed = { committed?.invoke() },
            committing = { committing?.invoke() },
            finishing = { finishing?.invoke() },
            recovering = { recovering?.invoke() },
            wipe = {
                wiping?.complete(it)
                wipeGate?.await(10, TimeUnit.SECONDS)
                it.deleteRecursively()
            },
        )
        return store.also { stores += it }
    }

    private fun DownloadStore.rows() = runBlocking { accountStore.db.downloads().all().associateBy { it.contentId } }

    private suspend fun DownloadStore.add(vararg ids: String) = enqueue(ids.map { NewDownload(it, "lantern", RequestedBy.USER) })

    private fun downloads(dir: File = temp.root) = File(dir, "downloads")

    private fun manifest(id: String, version: String = "v1", pages: Int = 2) =
        OfflineManifest(1, id, version, fileSize = 100, fileMtime = M1, pageCount = pages, from = 0, pages = List(pages) { OfflinePage("$it.png", width = 10, height = 15) })

    /** A whole transfer into the run's directory, published. */
    private suspend fun DownloadStore.download(run: Run, version: String, at: Long): Boolean {
        val m = manifest(run.contentId, version)
        File(run.dir, MANIFEST).writeText(AppJson.encodeToString(OfflineManifest.serializer(), m))
        manifest(run, m, at)
        for (i in 0 until m.pageCount) {
            File(run.dir, "$i").writeText("page $i of $version")
            page(run, i, 13)
        }
        return publish(run, CopyMade(run.dir.walk().filter { it.isFile }.sumOf { it.length() }, cover = false, seriesCover = false))
    }

    private var seq = 0L

    /** The lanes, and the server states they acknowledged: what the rule reads. */
    private suspend fun VoltisDatabase.seen(vararg lanes: LaneEntity) {
        lanes().upsert(lanes.toList())
        importReading(lanes.map { ReadingSnapshot(it.contentId, ReadingState(status = it.status, seq = ++seq)) })
    }

    private fun lane(id: String, status: String?, unacked: Boolean = false) = LaneEntity(
        id, null, null, "lantern", ContentType.COMIC, id, null, !unacked, false, null, status, null, "{}", null, null, 0, 0, null, null,
        true, false, 0, false, status, "{}", null, 0,
    )

    /** The series "lantern" cached with [n] comic volumes v1..vN in order. */
    private suspend fun cachedSeries(db: VoltisDatabase, n: Int = 5, at: Long = System.currentTimeMillis()) {
        db.cache(listOf(Content("lantern", "Lantern", ContentType.COMIC_SERIES) to null), at) { true }
        db.cache((1..n).map { Content("v$it", "Vol $it", ContentType.COMIC, parentId = "lantern") to it - 1 }, at) { false }
    }

    private suspend fun failure(block: suspend () -> Unit) = try {
        block()
        null
    } catch (e: Exception) {
        e
    }

    /** What a screen reads sizes from: not before the recovery and the directory accounting are done. */
    @Test
    fun startedOnlyOnceTheStartupIsDone() = runBlocking {
        val gate = CompletableDeferred<Unit>()
        recovering = { gate.await() }
        val store = newStore()
        kotlinx.coroutines.delay(300)
        assertFalse(store.started.value)
        gate.complete(Unit)
        withTimeout(10_000) { store.started.first { it } }
        recovering = null
    }

    /** A bulk download reconciles each row as it is now, in one commit: all or nothing, and nothing for a stopped owner. */
    @Test
    fun aBulkQueueReconcilesRowsAsTheyAre() = runBlocking {
        withTimeout(10_000) {
            val db = db()
            val good = AppJson.encodeToString(OfflineManifest.serializer(), manifest("x"))
            fun row(id: String, state: String, copy: Boolean = false, stale: String? = null, error: String? = null): DownloadEntity {
                if (copy) {
                    File(downloads(), "c-$id").mkdirs()
                    File(downloads(), "c-$id/$MANIFEST").writeText(good)
                    repeat(2) { File(downloads(), "c-$id/$it").writeText("page") }
                }
                return DownloadEntity(
                    id, "lantern", state, error = error, errorKind = error?.let { ErrorKind.SERVER }, requestedBy = RequestedBy.USER, queuedAt = 1,
                    transferId = if (state == DownloadState.DONE) null else "t-$id",
                    copyId = if (copy) "c-$id" else null, copyVersion = if (copy) "v1" else null, copyPageCount = if (copy) 2 else null, copyBytes = 8,
                    damagedCopy = if (stale == Stale.DAMAGED) "c-$id" else null, stale = stale,
                )
            }
            db.downloads().insert(
                listOf(
                    row("failed", DownloadState.FAILED, error = "boom"),
                    row("failedCopy", DownloadState.FAILED, copy = true, stale = Stale.VERSION, error = "boom"),
                    row("doneStale", DownloadState.DONE, copy = true, stale = Stale.VERSION),
                    row("doneDamaged", DownloadState.DONE, copy = true, stale = Stale.DAMAGED),
                    row("doneCurrent", DownloadState.DONE, copy = true),
                    row("pausedReplacement", DownloadState.PAUSED, copy = true, stale = Stale.VERSION),
                    row("queued", DownloadState.QUEUED, error = "kept"),
                ),
            )
            val offered = listOf("failed", "failedCopy", "none", "doneStale", "doneDamaged", "doneCurrent", "pausedReplacement", "queued")
                .map { NewDownload(it, "lantern", RequestedBy.USER) }
            val store = newStore(db = db)

            // Nothing is applied when the commit is refused (the second transition, a new transfer, can't be named), or the offer is over the limit.
            val before = store.rows()
            assertEquals(BulkQueued(5, true), store.queueBulk(offered, limit = 4))
            failId = true
            assertTrue(failure { store.queueBulk(offered, limit = 500) } is StorageFullException)
            failId = false
            assertEquals(before, store.rows())

            assertEquals(BulkQueued(5, false), store.queueBulk(offered, limit = 500))
            val rows = store.rows()
            val states = offered.associate { it.contentId to rows.getValue(it.contentId) }
            assertEquals(
                mapOf(
                    "failed" to DownloadState.QUEUED, "failedCopy" to DownloadState.QUEUED, "none" to DownloadState.QUEUED, "doneStale" to DownloadState.QUEUED,
                    "doneDamaged" to DownloadState.QUEUED, "doneCurrent" to DownloadState.DONE, "pausedReplacement" to DownloadState.PAUSED, "queued" to DownloadState.QUEUED,
                ),
                states.mapValues { it.value.state },
            )
            assertNull(states.getValue("failed").error)
            assertEquals("c-failedCopy", states.getValue("failedCopy").copyId)
            assertEquals("kept", states.getValue("queued").error)
            assertEquals(before.getValue("pausedReplacement"), states.getValue("pausedReplacement"))
            // Everything is present now.
            assertEquals(BulkQueued(0, false), store.queueBulk(offered, limit = 500))
            assertEquals(offered.map { it.contentId }.toSet(), store.presentIds())

            // A stopped owner applies nothing.
            val stopped = newStore(temp.newFolder("stopped"), db())
            stopped.stop()
            assertTrue(failure { stopped.queueBulk(offered, limit = 500) } is SyncUnavailable.AccountChanged)
            assertTrue(stopped.rows().isEmpty())
        }
    }

    /** Automatic downloads (P2 §17): one commit queues, offers and deletes, and the rule waits for what could still be read or unsent. */
    @Test
    fun reconcileAutoAppliesTheRuleInOneCommit() = runBlocking {
        withTimeout(10_000) {
            val db = db()
            val store = newStore(db = db)
            cachedSeries(db)
            val auto = db.auto()

            assertEquals(AutoApplied(2, 0), store.setPolicy("lantern", SeriesPolicy("lantern", 2, true)))
            assertEquals(mapOf("v1" to RequestedBy.AUTO, "v2" to RequestedBy.AUTO), store.rows().mapValues { it.value.requestedBy })
            assertEquals(setOf("v1", "v2"), auto.offers("lantern").toSet())
            // A second run changes nothing.
            val steady = store.rows()
            assertEquals(AutoApplied(0, 0), store.reconcileAuto())
            assertEquals(steady, store.rows())

            // v1 and v2 are downloaded and finished: a refused commit leaves rows, offers, policy and copies as they were.
            repeat(2) { store.runNext { run -> store.download(run, "v1", 10) } }
            for (id in listOf("v1", "v2")) db.seen(lane(id, "completed"))
            val before = store.rows()
            committing = { throw StorageFullException() }
            assertTrue(failure { store.reconcileAuto() } is StorageFullException)
            committing = null
            assertEquals(before, store.rows())
            assertEquals(setOf("v1", "v2"), auto.offers("lantern").toSet())
            assertEquals(2, auto.policy("lantern")!!.keepNext)
            assertTrue(File(downloads(), before.getValue("v1").copyId!!).isDirectory)

            // The engine holds an unstored mutation: the finished copies stay, and the window is still queued.
            val dir = temp.root.name
            ReadingHolds.set(dir, true)
            try {
                assertEquals(AutoApplied(2, 0), store.reconcileAuto())
            } finally {
                ReadingHolds.set(dir, false)
            }
            assertEquals(setOf("v1", "v2", "v3", "v4"), store.rows().keys)

            // A reader pins v2's copy: it stays until the pin is released, which says so.
            val pin = store.openCopy("v2", Job())!!
            val unpinned = store.unpinned.value
            assertEquals(AutoApplied(0, 1), store.reconcileAuto())
            assertEquals(setOf("v2", "v3", "v4"), store.rows().keys)
            pin.release()
            store.unpinned.first { it > unpinned }
            assertEquals(AutoApplied(0, 1), store.reconcileAuto())
            assertEquals(setOf("v3", "v4"), store.rows().keys)
            // The series' rows stay cached while the policy is there.
            assertEquals(5, db.content().volumes("lantern").size)

            // A manual delete of an offered volume sticks, though the window still covers it.
            store.delete(listOf("v3"))
            assertEquals(AutoApplied(0, 0), store.reconcileAuto())
            assertEquals(setOf("v4"), store.rows().keys)

            // Removing the policy cancels the automatic rows without a copy, forgets the offers, and keeps the copies.
            store.runNext { run -> store.download(run, "v1", 10) }
            store.enqueue(listOf(NewDownload("v5", "lantern", RequestedBy.AUTO)))
            store.add("v3")
            store.setPolicy("lantern", null)
            assertEquals(setOf("v3", "v4"), store.rows().keys)
            assertTrue(auto.offers("lantern").isEmpty())
            assertNull(auto.policy("lantern"))

            store.stop()
            assertTrue(failure { store.reconcileAuto() } is SyncUnavailable.AccountChanged)
        }
    }

    /** `keep` is set by the user's requests for a completed volume, in the accepted transaction only, and survives publication. */
    @Test
    fun keepIsSetOnlyByTheUsersRequestsForCompletedVolumes() = runBlocking {
        withTimeout(10_000) {
            val db = db()
            val store = newStore(db = db)
            cachedSeries(db, 7)
            for (id in listOf("v1", "v2", "v3", "v4", "v6", "v7")) db.seen(lane(id, "completed"))
            db.seen(lane("v5", "reading"))
            fun keep() = store.rows().filterValues { it.keep }.keys

            /** One row queued by [by] and downloaded; the queue is empty before and after. */
            suspend fun downloaded(id: String, by: String) {
                store.enqueue(listOf(NewDownload(id, "lantern", by)))
                store.runNext { run -> store.download(run, "v1", 10) }
            }

            // A new row for a completed volume; one for a volume still being read.
            downloaded("v1", RequestedBy.USER)
            downloaded("v5", RequestedBy.USER)
            assertEquals(setOf("v1"), keep())
            // An automatic row isn't kept, and an existing done row is by the user's request.
            downloaded("v2", RequestedBy.AUTO)
            assertFalse(store.rows().getValue("v2").keep)
            store.add("v2")
            assertEquals(setOf("v1", "v2"), keep())

            // A bulk selection with a current done copy among it: refused over the limit, then accepted.
            downloaded("v3", RequestedBy.AUTO)
            val selection = listOf("v3", "v4").map { NewDownload(it, "lantern", RequestedBy.USER) }
            assertEquals(BulkQueued(1, true), store.queueBulk(selection, limit = 0))
            assertEquals(setOf("v1", "v2"), keep())
            assertFalse("v4" in store.rows())
            assertEquals(BulkQueued(1, false), store.queueBulk(selection, limit = 5))
            assertEquals(setOf("v1", "v2", "v3", "v4"), keep())
            store.runNext { run -> store.download(run, "v1", 10) }

            // Retry of a failed row, and Download again of a stale copy.
            store.enqueue(listOf(NewDownload("v6", "lantern", RequestedBy.AUTO)))
            store.runNext { run -> store.end(run, RunEnd.Failed(ErrorKind.SERVER, "boom")) }
            assertFalse(store.rows().getValue("v6").keep)
            store.retry("v6")
            downloaded("v7", RequestedBy.AUTO)
            store.observe(listOf(Observation("v7", ServerFile(true, M2, 200), 20)))
            store.again("v7")
            assertEquals(setOf("v1", "v2", "v3", "v4", "v6", "v7"), keep())
            // Kept through the replacement's publication.
            runNextQueuedUntilDone(store)
            assertTrue(store.rows().getValue("v7").keep)
        }
    }

    private suspend fun runNextQueuedUntilDone(store: DownloadStore) {
        while (store.head() != null) store.runNext { run -> store.download(run, "v2", 30) }
    }

    /** A bulk download for A when the open account switches to B at each suspension: nothing for B, no late scheduling for A, nothing reported. */
    @Test
    fun aBulkQueueStopsWhenItsOwnerCloses() = runBlocking {
        withTimeout(10_000) {
            val items = listOf("a1", "a2").map { Content(it, "Vol $it", ContentType.COMIC, parentId = "series") }
            val b = newStore(temp.newFolder("b"))
            for (switchAt in listOf(null, "cache", "commit", "lookup")) {
                val a = newStore(temp.newFolder("a-$switchAt"))
                var open: DownloadStore? = a
                val events = mutableListOf<String>()
                suspend fun switch(at: String) {
                    if (at == switchAt) {
                        open = b
                        a.stop()
                        events += "stopped"
                    }
                }
                val starter = QueueStarter(
                    { open }, { true }, { switch("lookup"); false },
                    { store, _, _ -> events += "enqueue ${store.account}" }, { events += "cancel" },
                )
                val queue = BulkQueue({ open }, { _, _ -> switch("cache") }, { store -> switch("commit"); starter.start(owner = store) })
                val result = queue.queue(a, items)
                // Teardown cancels after any start that was in flight.
                starter.stop()
                val label = "$switchAt"
                if (switchAt == null) {
                    assertEquals(label, BulkQueued(2, false), result)
                    assertEquals(label, listOf("enqueue a", "cancel"), events)
                } else {
                    assertNull(label, result)
                    assertEquals(label, listOf("stopped", "cancel"), events)
                }
                assertTrue(label, b.rows().isEmpty())
                assertEquals(label, if (switchAt == "cache") 0 else 2, a.rows().size)
            }
        }
    }

    @Test
    fun controlsWinOverAHeldRun() = runBlocking {
        withTimeout(10_000) {
            val store = newStore()
            store.add("lantern-1", "lantern-2")
            val claimed = CompletableDeferred<Run>()
            val worker = async(Dispatchers.IO) {
                store.runNext { run ->
                    store.manifest(run, manifest(run.contentId), 10)
                    store.page(run, 0, 4)
                    claimed.complete(run)
                    awaitCancellation()
                }
            }
            val run = claimed.await()

            // Pause commits; its caller is cancelled before the run is revoked, which happens all the same. Later reports of the run are dropped.
            val atHook = CompletableDeferred<Unit>()
            val release = CompletableDeferred<Unit>()
            committed = {
                atHook.complete(Unit)
                release.await()
            }
            val pause = launch(Dispatchers.IO) { store.pause("lantern-1") }
            atHook.await()
            pause.cancel()
            committed = null
            release.complete(Unit)
            pause.join()
            assertNull(worker.await()!!.result)
            assertNull(store.page(run, 1, 4))
            assertFalse(store.publish(run, CopyMade(4, cover = false, seriesCover = false)))
            assertEquals(DownloadState.PAUSED to 1, store.rows().getValue("lantern-1").let { it.state to it.pagesDone })

            // Resumed: a new run, with a new token; the old one stays refused.
            store.resume("lantern-1")
            val again = store.runNext { next ->
                assertNull(store.page(run, 1, 4))
                store.page(next, 1, 4)?.pagesDone
            }!!
            assertEquals(2, again.result)

            // Deleted while the run holds a cover write: it lands in the run's own directory, which goes once the run has ended.
            val writing = CompletableDeferred<Run>()
            val written = CompletableDeferred<Unit>()
            val third = async(Dispatchers.IO) {
                store.runNext { next ->
                    writing.complete(next)
                    withContext(NonCancellable) {
                        written.await()
                        File(next.dir, COVER).writeText("cover")
                    }
                }
            }
            val held = writing.await()
            store.delete(listOf("lantern-1"))
            assertTrue(held.dir.isDirectory)
            wipeGate = CountDownLatch(1)
            wiping = CompletableDeferred()
            written.complete(Unit)
            third.await()
            assertEquals(File(downloads(), ".trash/${held.dir.name}"), wiping!!.await())
            assertEquals(listOf(".trash"), downloads().list()!!.toList())
            assertTrue(store.detachedBytes.value > 0)
            wipeGate!!.countDown()
            store.detachedBytes.first { it == 0L }
            assertEquals(emptyList<String>(), File(downloads(), ".trash").list()!!.toList())

            // A notification's Cancel for the deleted transfer does nothing to the new download.
            store.add("lantern-1")
            store.cancel("lantern-1", expect = held.dir.name)
            assertEquals(DownloadState.QUEUED, store.rows()["lantern-1"]?.state)

            // A full disk fails the item and pauses the rest of the queue.
            store.runNext { next -> store.end(next, RunEnd.StorageFull(DownloadError.NO_SPACE)) }
            val rows = store.rows()
            assertEquals(DownloadState.FAILED to ErrorKind.STORAGE, rows.getValue("lantern-2").let { it.state to it.errorKind })
            assertEquals(DownloadState.PAUSED, rows.getValue("lantern-1").state)
        }
    }

    @Test
    fun aReplacementLeavesThePinnedCopyCountedUntilItIsWiped() = runBlocking {
        withTimeout(10_000) {
            val store = newStore()
            store.add("lantern-1")
            store.runNext { store.download(it, "v1", at = 10) }
            val first = store.rows().getValue("lantern-1")
            val a = File(downloads(), first.copyId!!)
            // A reader holds A; the server has another version.
            val reader = Job()
            val pin = store.openCopy("lantern-1", reader)!!
            assertEquals(a, pin.dir)
            store.observe(listOf(Observation("lantern-1", ServerFile(true, M2, 120), at = 20)))
            store.again("lantern-1")
            assertTrue(store.runNext { store.download(it, "v2", at = 30) }!!.result!!)

            // B is the copy; A stays, and both are counted.
            val second = store.rows().getValue("lantern-1")
            assertNotEquals(first.copyId, second.copyId)
            val other = Job()
            assertEquals(second.copyId, store.openCopy("lantern-1", other)!!.copyId)
            other.complete()
            assertFalse(Pins.pinned(PinKey(temp.root.name, second.copyId!!)))
            assertTrue(a.isDirectory)
            assertEquals(first.copyBytes, store.detachedBytes.value)

            // The reader's composition still holds A after its scope ends; once that goes, A goes, counted until wiped.
            val hold = pin.hold()!!
            reader.complete()
            assertTrue(a.isDirectory)
            wipeGate = CountDownLatch(1)
            wiping = CompletableDeferred()
            hold.close()
            assertEquals(a.name, wiping!!.await().name)
            assertEquals(first.copyBytes, store.detachedBytes.value)
            assertNull(pin.hold())
            // Collected again while in the trash (a pin freed during the command that trashed it): still counted.
            store.collectSoon(a.name).join()
            assertEquals(first.copyBytes, store.detachedBytes.value)
            wipeGate!!.countDown()
            store.detachedBytes.first { it == 0L }
            assertFalse(a.exists())

            // A cancelled transfer's pages are counted until its producer has ended.
            store.add("lantern-2")
            val started = CompletableDeferred<Run>()
            val go = CompletableDeferred<Unit>()
            val worker = async(Dispatchers.IO) {
                store.runNext { run ->
                    store.manifest(run, manifest(run.contentId), 40)
                    File(run.dir, "0").writeText("page")
                    store.page(run, 0, 4)
                    started.complete(run)
                    withContext(NonCancellable) { go.await() }
                }
            }
            val run = started.await()
            store.cancel("lantern-2")
            assertNull(store.rows()["lantern-2"])
            assertEquals(4L, store.detachedBytes.value)
            assertTrue(run.dir.isDirectory)
            go.complete(Unit)
            worker.await()
            store.detachedBytes.first { it == 0L }
            assertFalse(run.dir.exists())
        }
    }

    @Test
    fun theStartCollectsLeftoversAndValidatesCopies() = runBlocking {
        withTimeout(10_000) {
            val db = db()
            val root = downloads()
            fun copy(id: String, manifest: String?, pages: Int) {
                File(root, id).mkdirs()
                manifest?.let { File(root, "$id/$MANIFEST").writeText(it) }
                repeat(pages) { File(root, "$id/$it").writeText("page") }
            }
            val good = AppJson.encodeToString(OfflineManifest.serializer(), manifest("x"))
            copy("c-ok", good, 2)
            copy("c-missing", good, 1)
            copy("c-unparsable", "{", 2)
            copy("t-running", null, 0)
            copy("orphan", null, 1)
            copy("pinned", null, 1)
            File(root, ".trash/old").mkdirs()
            fun done(id: String, copy: String) = DownloadEntity(
                id, "lantern", DownloadState.DONE, requestedBy = RequestedBy.USER, queuedAt = 1,
                copyId = copy, copyVersion = "v1", copyPageCount = 2, copyBytes = 8,
            )
            db.downloads().insert(
                listOf(
                    done("ok", "c-ok"), done("missing", "c-missing"), done("unparsable", "c-unparsable"),
                    DownloadEntity("running", "lantern", DownloadState.RUNNING, requestedBy = RequestedBy.USER, queuedAt = 1, transferId = "t-running"),
                ),
            )
            val pinned = PinKey(temp.root.name, "pinned")
            Pins.add(pinned)
            try {
                val store = newStore(db = db)
                assertNull(store.openCopy("running", Job()))
                store.detachedBytes.first { it == 4L }
                val rows = store.rows()
                assertEquals(DownloadState.DONE, rows.getValue("ok").state)
                for (id in listOf("missing", "unparsable")) {
                    val row = rows.getValue(id)
                    assertEquals(id, listOf(DownloadState.FAILED, DownloadError.MISSING_FILES, null), listOf(row.state, row.error, row.copyId))
                }
                assertEquals(DownloadState.QUEUED, rows.getValue("running").state)
                assertEquals(listOf(".trash", "c-ok", "pinned", "t-running"), root.list()!!.sorted())
                assertEquals(emptyList<String>(), File(root, ".trash").list()!!.toList())
            } finally {
                Pins.remove(pinned)
            }

            // A start that fails keeps every file, and its commands say so.
            val other = temp.newFolder("other")
            File(other, "downloads/kept").mkdirs()
            File(other, "downloads/kept/0").writeText("page")
            val broken = db().also { it.close() }
            val failed = newStore(other, broken)
            assertTrue(failure { failed.openCopy("ok", Job()) } is SyncUnavailable.OfflineData)
            assertTrue(failed.failed.value)
            assertTrue(File(other, "downloads/kept/0").isFile)
            failed.stop()
            assertTrue(failure { failed.openCopy("ok", Job()) } is SyncUnavailable.AccountChanged)

            // Stopped while the start runs: it returns once the start has, and commands say the account changed.
            val third = temp.newFolder("third")
            File(third, "downloads/.trash/old").mkdirs()
            wipeGate = CountDownLatch(1)
            wiping = CompletableDeferred()
            val starting = newStore(third)
            wiping!!.await()
            val stopping = async(Dispatchers.IO) { starting.stop() }
            wipeGate!!.countDown()
            stopping.await()
            assertTrue(failure { starting.openCopy("ok", Job()) } is SyncUnavailable.AccountChanged)

            // A start whose recovery can't be stored (a full disk) is ready all the same: rows as stored, every directory kept, every copy readable.
            val fifth = temp.newFolder("fifth")
            File(fifth, "downloads/c-short").apply { mkdirs() }.resolve(MANIFEST).writeText(good)
            val refused = db()
            refused.downloads().insert(
                listOf(
                    done("short", "c-short"),
                    DownloadEntity("again", "lantern", DownloadState.RUNNING, requestedBy = RequestedBy.USER, queuedAt = 1, transferId = "t-again"),
                ),
            )
            recovering = { throw StorageFullException() }
            val unrecovered = newStore(fifth, refused)
            assertNotNull(unrecovered.openCopy("short", Job()))
            assertFalse(unrecovered.failed.value)
            assertEquals(listOf("c-short"), File(fifth, "downloads").list()!!.filter { it != ".trash" })
            assertEquals(DownloadState.DONE, unrecovered.rows().getValue("short").state)
            recovering = null

            // A worker that starts while the start is held, with only a row a killed process left running: it gets that row, requeued, and transfers it.
            val fourth = temp.newFolder("fourth")
            File(fourth, "downloads/.trash/old").mkdirs()
            val killed = db()
            killed.downloads().insert(listOf(DownloadEntity("lantern-9", "lantern", DownloadState.RUNNING, requestedBy = RequestedBy.USER, queuedAt = 1, transferId = "t-9")))
            wipeGate = CountDownLatch(1)
            wiping = CompletableDeferred()
            val resuming = newStore(fourth, killed)
            wiping!!.await()
            val head = async(Dispatchers.IO) { resuming.head() }
            // Read past the store, the queue looks empty.
            assertNull(killed.downloads().next())
            wipeGate!!.countDown()
            assertEquals("lantern-9" to DownloadState.QUEUED, head.await()!!.let { it.contentId to it.state })
            assertEquals("t-9", resuming.runNext { it.dir.name }!!.result)
        }
    }

    @Test
    fun producersEndBeforeTheirCallerAndTheirEndIsRecorded() = runBlocking {
        withTimeout(10_000) {
            // A caller cancelled while its producer is in a blocking write resumes once the write has returned; the next run waits for it.
            val store = newStore()
            store.add("lantern-1")
            val writing = CompletableDeferred<Pair<Run, Job>>()
            val write = CountDownLatch(1)
            val first = launch(Dispatchers.IO) {
                store.runNext { run ->
                    writing.complete(run to currentCoroutineContext().job)
                    write.await(10, TimeUnit.SECONDS)
                    File(run.dir, "0").writeText("late")
                }
            }
            val (run, producer) = writing.await()
            first.cancel()
            // The producer is the caller's child: cancelled with it, and the caller can't end before it.
            assertTrue(producer.isCancelled)
            // Undispatched on the dispatcher the store's commands use: this call returns with the second caller waiting for the producer still ending.
            val second = async(Dispatchers.IO, CoroutineStart.UNDISPATCHED) { store.runNext { it to File(it.dir, "0").readText() } }
            assertEquals(listOf(false, false, false), listOf(first.isCompleted, producer.isCompleted, second.isCompleted))
            assertEquals(emptyList<String>(), run.dir.list()!!.toList())
            write.countDown()
            first.join()
            // The second run began only after the write: on the same transfer, with the page in place.
            assertEquals(run.dir to "late", second.await()!!.result!!.let { it.first.dir to it.second })
            assertEquals(DownloadState.QUEUED, store.rows().getValue("lantern-1").state)

            // Cancelled before its producer first runs: it still ends, and the next run goes.
            val gate = HeldDispatcher()
            val held = newStore(temp.newFolder("held"), io = gate)
            held.add("lantern-2")
            val caller = launch(Dispatchers.IO) {
                held.runNext {
                    currentCoroutineContext().ensureActive()
                    it
                }
            }
            gate.queued.await()
            caller.cancel()
            gate.open()
            caller.join()
            assertEquals("lantern-2", held.runNext { it.contentId }!!.result)

            // A directory that can't be made fails only its item.
            var next = 0
            val failing = newStore(temp.newFolder("failing"), newId = { if (next++ == 0) "missing/dir" else "d$next" })
            failing.add("lantern-3", "lantern-4")
            assertEquals("lantern-4", failing.runNext { it.contentId }!!.result)
            val rows = failing.rows()
            assertEquals(listOf(DownloadState.FAILED, ErrorKind.STORAGE, DownloadError.WRITE_FAILED), rows.getValue("lantern-3").let { listOf(it.state, it.errorKind, it.error) })
            assertEquals(DownloadState.QUEUED, rows.getValue("lantern-4").state)

            // An end that can't be recorded: the caller waits for it, then hears it.
            val atFinish = CompletableDeferred<Unit>()
            val fail = CompletableDeferred<Unit>()
            finishing = {
                atFinish.complete(Unit)
                fail.await()
                error("disk")
            }
            val persisting = async(Dispatchers.IO) { failure { store.runNext { it } } }
            atFinish.await()
            assertFalse(persisting.isCompleted)
            fail.complete(Unit)
            assertTrue(persisting.await() is DownloadsPersistenceFailed)
            finishing = null

            // The registry: a store mapped after the switch away from it gets no owner; the next one does.
            val a = AccountStore("a", temp.newFolder("a"), db())
            val b = AccountStore("b", temp.newFolder("b"), db())
            val current = MutableStateFlow<AccountStore?>(null)
            val atMapping = CompletableDeferred<Unit>()
            val mapped = CompletableDeferred<Unit>()
            val created = mutableListOf<String>()
            val owners = DownloadOwners(
                current, scope,
                create = { s, onFree -> DownloadStore(s, scope, onFree).also { created += s.account } },
                mapping = {
                    if (it === a) {
                        atMapping.complete(Unit)
                        mapped.await()
                    }
                },
            )
            current.value = a
            atMapping.await()
            current.value = null
            owners.stop()
            current.value = b
            mapped.complete(Unit)
            assertEquals("b", owners.current.first { it != null }!!.account)
            assertEquals(listOf("b"), created)
            owners.stop()
        }
    }

    /** Effects follow their commit: a refused restart keeps the run's identity; a failed claim is a persistence failure; cancel and the start prune unowned content. */
    @Test
    fun effectsFollowTheirCommitAndUnownedContentGoes() = runBlocking {
        withTimeout(10_000) {
            val store = newStore()
            store.add("lantern-1")
            val wakes = store.wakes.value
            assertTrue(wakes > 0)
            store.runNext { run ->
                failId = true
                assertNotNull(failure { store.restart(run) })
                failId = false
                // The row, the live run and the directories still agree.
                assertEquals(run.dir.name, store.rows().getValue("lantern-1").transferId)
                assertNotNull(store.page(run, 0, 4))
                assertEquals(listOf(run.dir.name), downloads().list()!!.filter { it != ".trash" })
                val next = store.restart(run)!!
                assertEquals(next.dir.name, store.rows().getValue("lantern-1").transferId)
                assertTrue(next.dir.isDirectory)
            }
            // Pages and ends are no wake.
            assertEquals(wakes, store.wakes.value)

            // The claim fails before any producer exists: the worker gets a persistence failure, and the row stays queued.
            val sql = store.accountStore.db.openHelper.writableDatabase
            sql.execSQL("CREATE TRIGGER refuse BEFORE UPDATE ON download WHEN NEW.state = '${DownloadState.RUNNING}' BEGIN SELECT RAISE(ABORT, 'disk full'); END")
            assertTrue(failure { store.runNext { it } } is DownloadsPersistenceFailed)
            sql.execSQL("DROP TRIGGER refuse")
            assertEquals(DownloadState.QUEUED, store.rows().getValue("lantern-1").state)

            // Cancelling the last row leaves no cached series behind.
            store.delete(listOf("lantern-1"))
            val db = store.accountStore.db
            cachedSeries(db)
            store.add("v1")
            store.cancel("v1")
            assertTrue(db.content().childIds("lantern").isEmpty())

            // Content nothing owns, from an interrupted queueing, goes at the next start.
            val other = db()
            cachedSeries(other, at = 1)
            newStore(temp.newFolder("other"), other).head()
            assertTrue(other.content().childIds("lantern").isEmpty())
        }
    }

    /** A policy saved while the owner is still starting keeps the series it cached, and its window is queued at once. */
    @Test
    fun aBootstrapCachedDuringTheStartKeepsItsWindow() = runBlocking {
        withTimeout(10_000) {
            val dir = temp.newFolder("starting")
            File(downloads(dir), ".trash/old").mkdirs()
            wipeGate = CountDownLatch(1)
            wiping = CompletableDeferred()
            val db = db()
            val store = newStore(dir, db)
            // The start is held before its recovery and prune; the series is fetched meanwhile.
            wiping!!.await()
            cachedSeries(db)
            val saved = async(Dispatchers.IO) { store.setPolicy("lantern", SeriesPolicy("lantern", 2, false)) }
            wipeGate!!.countDown()
            assertEquals(AutoApplied(2, 0), saved.await())
            assertEquals(setOf("v1", "v2"), store.rows().keys)
        }
    }

    /** Queues what it is given until [open]; then runs everything on the IO dispatcher. */
    private class HeldDispatcher : CoroutineDispatcher() {
        val queued = CompletableDeferred<Unit>()
        private val held = mutableListOf<Runnable>()
        private var opened = false

        override fun dispatch(context: CoroutineContext, block: Runnable) {
            val now = synchronized(this) {
                if (!opened) held += block
                opened
            }
            if (now) Dispatchers.IO.dispatch(context, block) else queued.complete(Unit)
        }

        fun open() {
            val blocks = synchronized(this) {
                opened = true
                held.toList().also { held.clear() }
            }
            blocks.forEach { Dispatchers.IO.dispatch(Dispatchers.IO, it) }
        }
    }

    private companion object {
        const val M1 = "2026-01-10T06:00:00Z"
        const val M2 = "2026-02-10T06:00:00Z"
    }
}
