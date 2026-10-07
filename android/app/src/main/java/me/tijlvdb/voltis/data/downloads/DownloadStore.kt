package me.tijlvdb.voltis.data.downloads

import android.util.Log
import java.io.File
import java.util.UUID
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.DelicateCoroutinesApi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.merge
import kotlinx.coroutines.flow.take
import kotlinx.coroutines.job
import kotlinx.coroutines.joinAll
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.db.AutoOfferEntity
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.data.db.SeriesPolicyEntity
import me.tijlvdb.voltis.data.db.pruneSnapshots
import me.tijlvdb.voltis.data.reading.ReadingHolds
import me.tijlvdb.voltis.domain.downloads.RequestedBy
import me.tijlvdb.voltis.domain.downloads.SeriesPolicy
import me.tijlvdb.voltis.domain.downloads.autoPlan
import me.tijlvdb.voltis.domain.downloads.DownloadError
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.ErrorKind
import me.tijlvdb.voltis.data.storage.localTransaction
import me.tijlvdb.voltis.domain.downloads.ServerFile
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.domain.reading.SyncUnavailable

/** A copy's or transfer's manifest, in its directory. */
internal const val MANIFEST = "manifest.json"
internal const val COVER = "cover.jpg"
internal const val SERIES_COVER = "series.jpg"
private const val COMPLETED = "completed"

/** A new directory's name: random, so no path is ever used twice. */
fun randomDirId(): String = UUID.randomUUID().toString().replace("-", "")

/**
 * One account store's downloads (P2 §8): the only writer of its download rows, and the only code
 * that creates, retires or deletes anything under its `downloads/`. Commands run one at a time;
 * once a command holds the lock, its commit and every effect finish even if its caller is
 * cancelled. Network traffic and page bytes stay outside: producers write their own transfer
 * directory and report through [manifest], [page], [restart], [publish] and [end], which apply
 * only while their run is the live one and its row is still running on that transfer.
 *
 * Directories are immutable once they are a copy, never reused, and named by the row columns
 * that reference them. A directory nothing references, pins or writes is moved to `.trash/` and
 * wiped in the background; `.trash/` is wiped entirely at the start.
 */
class DownloadStore internal constructor(
    val accountStore: AccountStore,
    parent: CoroutineScope,
    private val onFree: (PinKey) -> Unit,
    private val clock: () -> Long = System::currentTimeMillis,
    private val newId: () -> String = ::randomDirId,
    /** Where producers run. */
    private val io: CoroutineDispatcher = Dispatchers.IO,
    /** Runs after a control's transaction, before its revocations and collections (tests). */
    private val committed: suspend () -> Unit = {},
    /** Runs inside an automatic command's transaction, after its steps: throwing refuses the whole commit (tests). */
    private val committing: suspend () -> Unit = {},
    /** Runs in `finish`'s transaction (tests). */
    private val finishing: suspend () -> Unit = {},
    /** Runs in the start's recovery transaction (tests). */
    private val recovering: suspend () -> Unit = {},
    /** Deletes a collected directory; false leaves it for the next wipe. */
    private val wipe: (File) -> Boolean = File::deleteRecursively,
) {
    val account: String get() = accountStore.account
    private val db = accountStore.db
    private val dao = db.downloads()
    private val root = File(accountStore.dir, "downloads")
    private val trash = File(root, TRASH)
    private val accountDir = accountStore.dir.name

    /** When this owner started: content fetched after it may belong to a queueing in flight. */
    private val startedAt = clock()

    /** Init, collections and wipes. */
    private val scope = CoroutineScope(SupervisorJob(parent.coroutineContext[Job]) + Dispatchers.IO)
    private val ready = CompletableDeferred<Unit>()
    private val lock = Mutex()

    @Volatile private var closed = false
    private val stopped = CompletableDeferred<Unit>()
    private val live = HashMap<String, LiveRun>()
    private var tokens = 0L
    private val wipes = Channel<Unit>(Channel.CONFLATED)

    /** Directories on disk that no row references, with their sizes: pinned former copies, transfers still ending, `.trash` not yet wiped. */
    private val detached = HashMap<String, Long>()
    private val _detachedBytes = MutableStateFlow(0L)
    val detachedBytes: StateFlow<Long> = _detachedBytes.asStateFlow()

    /** Series whose content a queueing or bootstrap in flight has cached and its owner command hasn't consumed: no prune takes them. Guarded by itself. */
    private val reserved = HashMap<String, Int>()

    private val _wakes = MutableStateFlow(0L)
    private val serviced = java.util.concurrent.atomic.AtomicLong(0)

    /**
     * Incremented by every control or automatic commit that made a row runnable (queued it, or resumed or retried it).
     * It is raised inside the command, so a caller's cancellation can't lose it; the owner's follower
     * schedules the work from it. Conflated: only a change matters.
     */
    val wakes: StateFlow<Long> = _wakes.asStateFlow()

    /** The latest wake whose work was submitted successfully, by the caller or the follower. */
    internal val servicedWakes: Long get() = serviced.get()

    /** Wake [upTo] and every one before it have been submitted. */
    internal fun service(upTo: Long) {
        serviced.accumulateAndGet(upTo, ::maxOf)
    }

    private val _started = MutableStateFlow(false)

    /** The start is done: rows recovered, copies validated, leftover directories accounted in [detachedBytes]. A failed start never sets it ([failed]). */
    val started: StateFlow<Boolean> = _started.asStateFlow()

    private val _failed = MutableStateFlow(false)

    /** The start failed: the files are kept, and commands throw `OfflineData`. */
    val failed: StateFlow<Boolean> = _failed.asStateFlow()

    private class LiveRun(val token: Long, var dirId: String, val job: Job) {
        var revoked = false

        /** The run's own observation, from its stream's manifest. */
        var manifestAt: Long? = null
        var manifest: ServerFile? = null
    }

    init {
        scope.launch {
            try {
                lock.withLock { withContext(NonCancellable) { start() } }
                ready.complete(Unit)
                _started.value = true
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Log.e(TAG, "Couldn't start the downloads of $accountDir", e)
                _failed.value = true
                ready.completeExceptionally(SyncUnavailable.OfflineData())
                return@launch
            }
            wipes.trySend(Unit)
            for (unit in wipes) wipeTrash()
        }.invokeOnCompletion { if (!ready.isCompleted) ready.completeExceptionally(SyncUnavailable.AccountChanged()) }
    }

    // Controls. The facade wakes the queue after resume, resumeAll, retry and again.

    suspend fun enqueue(rows: List<NewDownload>) = control {
        for (new in rows) {
            step(new.contentId, Event.Enqueue(new))
            if (new.requestedBy == RequestedBy.USER) keepIfCompleted(new.contentId)
        }
    }

    /** The ids of rows a bulk download leaves alone: present already, or a copy that is current. */
    suspend fun presentIds(): Set<String> = command { dao.all().filter { bulkEvent(it) == null }.mapTo(HashSet()) { it.contentId } }

    /**
     * A bulk download (P4 §6), in one transaction: each of [rows] as its row is now. None is queued;
     * a failed one is retried, a stale or damaged copy downloaded again ([bulkEvent]); the rest is skipped.
     * More than [limit] offered applies nothing ([BulkQueued.tooMany]), and so does a refused commit.
     * The caller wakes the queue.
     */
    suspend fun queueBulk(rows: List<NewDownload>, limit: Int): BulkQueued {
        var queued = BulkQueued(0, false)
        control {
            val events = rows.distinctBy { it.contentId }.mapNotNull { new ->
                val row = dao.get(new.contentId)
                (if (row == null) Event.Enqueue(new) else bulkEvent(row))?.let { new.contentId to it }
            }
            if (events.size > limit) {
                queued = BulkQueued(events.size, true)
            } else {
                for ((id, event) in events) step(id, event)
                // The whole selection, rows left alone included.
                for (new in rows.distinctBy { it.contentId }) keepIfCompleted(new.contentId)
                queued = BulkQueued(events.size, false)
            }
        }
        return queued
    }

    suspend fun pause(id: String, expect: String? = null) = control { step(id, Event.Pause(expect)) }

    suspend fun resume(id: String) = control { step(id, Event.Resume) }

    suspend fun pauseAll() = control { for (row in dao.all()) step(row.contentId, Event.Pause(), row) }

    suspend fun resumeAll() = control { for (row in dao.all()) step(row.contentId, Event.ResumeAll, row) }

    suspend fun retry(id: String) = control {
        step(id, Event.Retry)
        keepIfCompleted(id)
    }

    suspend fun again(id: String) = control {
        step(id, Event.Again)
        keepIfCompleted(id)
    }

    suspend fun cancel(id: String, expect: String? = null) = control { step(id, Event.Cancel(expect)) }

    suspend fun delete(ids: Collection<String>) = control {
        for (id in ids) step(id, Event.Delete)
    }

    suspend fun deleteAll() = control {
        for (row in dao.all()) step(row.contentId, Event.Delete, row)
    }

    /** Gone from the server, as a request that started at [at] (wall clock) saw it. */
    suspend fun markGone(id: String, at: Long) = control { step(id, RunEnd.Gone(at)) }

    /** A reader found the files of [copyId] damaged. */
    suspend fun markDamaged(id: String, copyId: String) = control { step(id, Event.Damaged(copyId)) }

    suspend fun observe(observations: List<Observation>) = control { for (o in observations) step(o.contentId, Event.Observe(o.server, o.at)) }

    // Automatic downloads (P2 §17).

    /**
     * Makes the downloads of the series with a policy ([seriesIds], null: every one) match it: what the rule
     * says, read and applied in one transaction under the lock. All or nothing; the caller schedules the queue.
     */
    suspend fun reconcileAuto(seriesIds: Set<String>? = null): AutoApplied = command {
        var applied = AutoApplied(0, 0)
        commit(committed, wake = true) { applied = reconcile(seriesIds) }
        applied
    }

    /** Sets [seriesId]'s policy, or removes it (null) with its offers and its `auto` rows that have no copy, and reconciles it, in one transaction. */
    suspend fun setPolicy(seriesId: String, policy: SeriesPolicy?): AutoApplied = command {
        var applied = AutoApplied(0, 0)
        commit(committed, wake = true) {
            val auto = db.auto()
            if (policy == null) {
                auto.deletePolicy(seriesId)
                auto.deleteOffers(seriesId)
                for (row in dao.inSeries(seriesId)) if (row.requestedBy == RequestedBy.AUTO && row.copyId == null) step(row.contentId, Event.Cancel(), row)
                applied = reconcile(setOf(seriesId))
                pruneSeries(listOf(seriesId))
            } else {
                auto.upsertPolicy(SeriesPolicyEntity(seriesId, policy.keepNext, policy.deleteFinished, clock()))
                applied = reconcile(setOf(seriesId))
            }
        }
        applied
    }

    /** Held until released: the content cached for [seriesIds] survives every prune, for the owner command that follows the caching. */
    fun reserve(seriesIds: Collection<String>): Reservation {
        val ids = seriesIds.toSet()
        synchronized(reserved) { for (id in ids) reserved.merge(id, 1, Int::plus) }
        return Reservation {
            synchronized(reserved) { for (id in ids) reserved.computeIfPresent(id) { _, n -> if (n > 1) n - 1 else null } }
        }
    }

    /** Idempotent. Release in a `finally` after the owner command, whether it completed, failed or was cancelled. */
    class Reservation internal constructor(private val onRelease: () -> Unit) {
        private val released = java.util.concurrent.atomic.AtomicBoolean(false)

        fun release() {
            if (released.compareAndSet(false, true)) onRelease()
        }
    }

    private fun reservedNow(): Set<String> = synchronized(reserved) { reserved.keys.toSet() }

    /** Prunes what nothing owns of [seriesIds], except the series reserved, read now (inside the caller's transaction). */
    private suspend fun pruneSeries(seriesIds: Collection<String>) {
        val skip = reservedNow()
        // Below SQLite's oldest limit on bound parameters (999).
        seriesIds.filter { it !in skip }.chunked(500).forEach { db.content().prune(it) }
        db.pruneSnapshots()
    }

    /** The series whose plan isn't empty now, read without the lock: [reconcileAuto] for them decides under it. */
    suspend fun autoWork(): Set<String> {
        if (closed) throw SyncUnavailable.AccountChanged()
        ready.await()
        val work = LinkedHashSet<String>()
        for (p in db.auto().policies()) {
            val input = readAutoInput(db, p.seriesId, { isPinned(it) }, { readingHeld() }) ?: continue
            if (!autoPlan(input).isEmpty) work += p.seriesId
        }
        return work
    }

    /** How many finished volumes a policy with "delete after finishing" would delete now, read without the lock. */
    suspend fun finishedDeletable(seriesId: String): Int {
        if (closed) throw SyncUnavailable.AccountChanged()
        ready.await()
        val policy = SeriesPolicy(seriesId, 0, true)
        val input = readAutoInput(db, seriesId, { isPinned(it) }, { readingHeld() }, policy) ?: return 0
        return autoPlan(input).delete.size
    }

    private fun isPinned(copyId: String) = Pins.pinned(PinKey(accountDir, copyId))

    private fun readingHeld() = ReadingHolds.held(accountDir)

    private val _unpinned = MutableStateFlow(0L)

    /** Incremented when a reader's last hold on a copy went: a delete may be possible now. Never lost, conflated. */
    val unpinned: StateFlow<Long> = _unpinned.asStateFlow()

    // Reader.

    /** Pins the current copy of [id] until [owner] completes; null without one. */
    suspend fun openCopy(id: String, owner: Job): CopyPin? = command {
        val row = dao.get(id) ?: return@command null
        val copyId = row.copyId ?: return@command null
        val key = PinKey(accountDir, copyId)
        Pins.add(key)
        CopyPin(copyId, File(root, copyId), row.stale, row.copyPageCount ?: 0, row.copyBytes, key, onFree).also { pin -> owner.invokeOnCompletion { pin.release() } }
    }

    /** Emits once when [id]'s copy is no longer [copyId], the row deleted included; completes without emitting at [stop]. */
    fun changes(id: String, copyId: String): Flow<Unit> = merge(
        dao.observe(id).filter { it?.copyId != copyId }.map { true }.catch { },
        flow {
            stopped.await()
            emit(false)
        },
    ).take(1).filter { it }.map { }

    /** False once stopped. */
    val isOpen: Boolean get() = !closed

    /** Throws `AccountChanged` once stopped. */
    fun ensureOpen() {
        if (closed) throw SyncUnavailable.AccountChanged()
    }

    /** A pin's last hold went: [dirId] goes if nothing else needs it. */
    fun collectSoon(dirId: String): Job {
        _unpinned.value += 1
        return collectLater(dirId)
    }

    private fun collectLater(dirId: String): Job = scope.launch {
        try {
            command { collectOrLog(dirId) }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // Stopped or not started: the next start collects it.
        }
    }

    // Worker.

    /**
     * The queue's head, once the start has requeued what a killed process left running: the row
     * the next [runNext] claims, or one whose producer is still ending. Null when nothing is left to run.
     */
    suspend fun head(): DownloadEntity? = command { dao.next() ?: dao.running().firstOrNull() }

    /**
     * Claims the queue's head and runs [block] on it as a producer: a child of this call, started
     * so that its cleanup always runs. Returns once the producer has ended and its end is recorded;
     * null when nothing is queued. Throws [DownloadsPersistenceFailed] when that record, or the claim before it, failed.
     */
    suspend fun <T> runNext(block: suspend (Run) -> T): Ran<T>? = coroutineScope { claimAndRun(this, block) }

    // ATOMIC: the producer always enters produce(), so its finish() runs even if it is cancelled before it starts.
    @OptIn(DelicateCoroutinesApi::class)
    private suspend fun <T> claimAndRun(caller: CoroutineScope, block: suspend (Run) -> T): Ran<T>? {
        while (true) {
            val next = command {
                // A producer still ending, of any worker.
                live.values.firstOrNull()?.let { return@command Step.Busy(it.job) }
                persisting { commit { for (row in dao.running()) step(row.contentId, Event.Stranded, row) } }
                val head = dao.next() ?: return@command Step.Empty
                val row = checkNotNull(persisting { commit { step(head.contentId, Event.Claim, head) } })
                val dir = File(root, checkNotNull(row.transferId))
                if (!dir.isDirectory && !dir.mkdir()) {
                    Log.e(TAG, "Couldn't create $dir")
                    persisting { commit { step(row.contentId, RunEnd.Failed(ErrorKind.STORAGE, DownloadError.WRITE_FAILED), row) } }
                    return@command Step.Again
                }
                val run = Run(
                    account, row.contentId, row.seriesId, ++tokens, dir, row.version, row.pageCount, row.pagesDone, row.bytesDone, row.fileSize,
                    db.content().get(row.contentId)?.fileSize,
                )
                val slot = CompletableDeferred<Ran<T>>()
                val job = caller.launch(io, CoroutineStart.ATOMIC) { produce(run, block, slot) }
                live[row.contentId] = LiveRun(run.token, dir.name, job)
                Step.Started(job, slot)
            }
            when (next) {
                is Step.Busy -> next.job.join()
                Step.Empty -> return null
                Step.Again -> continue
                is Step.Started -> {
                    next.job.join()
                    @Suppress("UNCHECKED_CAST")
                    return next.slot.await() as Ran<T>
                }
            }
        }
    }

    /** The stream's manifest, seen by a request that started at [at]. Null: the run isn't admitted, so it stops. */
    suspend fun manifest(run: Run, m: OfflineManifest, at: Long): DownloadEntity? = report(run) { row, entry ->
        val after = step(row.contentId, Event.Manifest(m), row)
        // The run's observation is kept once the row has it.
        afterCommit {
            entry.manifestAt = at
            entry.manifest = ServerFile(true, m.fileMtime, m.fileSize)
        }
        after
    }

    /** Page [index] of [bytes] is in place. */
    suspend fun page(run: Run, index: Int, bytes: Long): DownloadEntity? = report(run) { row, _ ->
        step(row.contentId, Event.Page(index, bytes), row).takeIf { it !== row }
    }

    /** From page 0 again, into a new directory; the same run. */
    suspend fun restart(run: Run): Run? = command {
        val entry = admitted(run) ?: return@command null
        reporter = entry.token
        var old: DownloadEntity? = null
        val next = commit {
            val row = runningRow(run, entry) ?: return@commit null
            old = row
            checkNotNull(step(row.contentId, Event.Restarted, row))
        } ?: return@command null
        // Committed: the live identity and the directory follow the row, never ahead of it.
        val dir = File(root, checkNotNull(next.transferId))
        val previous = entry.dirId
        entry.dirId = dir.name
        collectOrLog(previous, old?.bytesDone)
        if (dir.mkdir()) {
            Run(run.account, run.contentId, run.seriesId, run.token, dir, null, null, 0, 0, null, run.cachedFileSize)
        } else {
            Log.e(TAG, "Couldn't create $dir")
            commit { step(run.contentId, RunEnd.Failed(ErrorKind.STORAGE, DownloadError.WRITE_FAILED), next) }
            null
        }
    }

    /** The transfer is complete and becomes the copy. */
    suspend fun publish(run: Run, made: CopyMade): Boolean = report(run) { row, entry ->
        step(row.contentId, Event.Published(made, entry.manifestAt, entry.manifest), row).takeIf { it !== row }
    } != null

    /** How the run ended. A full disk pauses the rest of the queue. */
    suspend fun end(run: Run, end: RunEnd): DownloadEntity? = report(run) { row, _ ->
        val after = step(row.contentId, end, row)
        if (end is RunEnd.StorageFull) for (other in dao.queued()) step(other.contentId, Event.Pause(), other)
        after
    }

    /** Before the account's store closes: commands throw `AccountChanged`, producers are cancelled and joined. */
    suspend fun stop() {
        if (!ready.isCompleted) ready.completeExceptionally(SyncUnavailable.AccountChanged())
        val jobs = lock.withLock {
            closed = true
            live.values.onEach { it.revoked = true }.map { it.job }
        }
        stopped.complete(Unit)
        jobs.forEach { it.cancel() }
        // Each producer's finish() takes the lock without the closed check.
        jobs.joinAll()
        scope.coroutineContext.job.cancelAndJoin()
    }

    private sealed interface Step {
        class Busy(val job: Job) : Step

        data object Empty : Step

        data object Again : Step

        class Started(val job: Job, val slot: CompletableDeferred<*>) : Step
    }

    private suspend fun <T> command(block: suspend Locked.() -> T): T {
        // AccountChanged once stopped, also after a failed start; else OfflineData when the start failed.
        if (closed) throw SyncUnavailable.AccountChanged()
        ready.await()
        return lock.withLock {
            withContext(NonCancellable + Dispatchers.IO) {
                if (closed) throw SyncUnavailable.AccountChanged()
                Locked().block()
            }
        }
    }

    private suspend fun control(block: suspend Locked.() -> Unit) = command { commit(committed, wake = true) { block() } }

    /** A run's report, applied only while it is admitted; null otherwise. */
    private suspend fun <T> report(run: Run, block: suspend Locked.(DownloadEntity, LiveRun) -> T?): T? = command {
        val entry = admitted(run) ?: return@command null
        reporter = entry.token
        commit {
            val row = runningRow(run, entry) ?: return@commit null
            block(row, entry)
        }
    }

    private fun admitted(run: Run): LiveRun? =
        live[run.contentId]?.takeIf { it.token == run.token && it.dirId == run.dir.name && !it.revoked }

    private suspend fun runningRow(run: Run, entry: LiveRun): DownloadEntity? =
        dao.get(run.contentId)?.takeIf { it.state == DownloadState.RUNNING && it.transferId == entry.dirId }

    /** A persistence failure before a producer exists: the worker retries (capped), as for a failed end. */
    private suspend fun <T> persisting(block: suspend () -> T): T = try {
        block()
    } catch (e: CancellationException) {
        throw e
    } catch (e: SyncUnavailable) {
        throw e
    } catch (e: Exception) {
        throw DownloadsPersistenceFailed(e)
    }

    private suspend fun <T> produce(run: Run, block: suspend (Run) -> T, slot: CompletableDeferred<Ran<T>>) {
        var result: T? = null
        var failure: Throwable? = null
        try {
            result = block(run)
        } catch (e: CancellationException) {
            // Revoked, stopped, or the worker cancelled: finish() records it.
        } catch (e: Throwable) {
            Log.e(TAG, "Download of ${run.contentId} failed", e)
            failure = e
        } finally {
            withContext(NonCancellable) {
                try {
                    slot.complete(lock.withLock { finish(run, result, failure) })
                } catch (e: Throwable) {
                    slot.completeExceptionally(DownloadsPersistenceFailed(e))
                }
            }
        }
    }

    /** Under the lock: the run leaves [live]; an admitted one that didn't report its end is requeued, or failed for [failure]; its directory is collected. */
    private suspend fun <T> finish(run: Run, result: T?, failure: Throwable?): Ran<T> = withContext(Dispatchers.IO) {
        val id = run.contentId
        val entry = live[id]?.takeIf { it.token == run.token }
        if (entry != null) live.remove(id)
        val locked = Locked()
        val row = try {
            locked.commit {
                finishing()
                val current = dao.get(id)
                val admitted = entry != null && !entry.revoked && current?.state == DownloadState.RUNNING && current.transferId == entry.dirId
                if (admitted) step(id, failure?.let { Event.Unexpected(it.toString()) } ?: Event.Ended, current) else current
            }
        } finally {
            entry?.let { locked.collectOrLog(it.dirId) }
        }
        Ran(result.takeIf { entry != null && !entry.revoked && failure == null }, row)
    }

    /** Under the lock and `NonCancellable`. */
    private inner class Locked {
        /** The rows the current commit changed: the row after, null when deleted. */
        private val after = HashMap<String, DownloadEntity?>()

        /** Directories the current commit dropped, with their known sizes. */
        private val dropped = HashMap<String, Long>()

        /** The token of the run whose report this is. */
        var reporter: Long? = null

        /** A row became runnable in the current commit. */
        private var queued = false

        /** The series of the download rows removed in the current commit. */
        private val removed = HashSet<String>()

        private val effects = ArrayList<() -> Unit>()

        /** In-memory effects of the current commit, applied once it has committed. */
        fun afterCommit(effect: () -> Unit) {
            effects += effect
        }

        /** One transaction; then revocations, then the dropped directories are collected. [committed] runs once the transaction has. */
        suspend fun <T> commit(committed: suspend () -> Unit = {}, wake: Boolean = false, block: suspend Locked.() -> T): T {
            after.clear()
            dropped.clear()
            effects.clear()
            queued = false
            removed.clear()
            val result = db.localTransaction {
                block().also {
                    // A removed row may have been the last to need its cached series: only those series.
                    if (removed.isNotEmpty()) pruneSeries(removed)
                }
            }
            effects.forEach { it() }
            if (wake && queued) _wakes.value += 1
            committed()
            for ((id, row) in after) {
                // A run's own report ends it by its return; the row refuses its later reports.
                val entry = live[id]?.takeIf { it.token != reporter } ?: continue
                if (!entry.revoked && (row == null || row.state != DownloadState.RUNNING || row.transferId != entry.dirId)) {
                    entry.revoked = true
                    entry.job.cancel()
                }
            }
            for ((dirId, size) in dropped) collectOrLog(dirId, size)
            return result
        }

        /** A failure leaves the directory, counted, for the next trigger or the next start. */
        suspend fun collectOrLog(dirId: String, knownSize: Long? = null) {
            try {
                collect(dirId, knownSize)
            } catch (e: Exception) {
                Log.e(TAG, "Couldn't collect $dirId", e)
                knownSize?.let { detach(dirId, it) }
            }
        }

        /** The rule's plan for each series, applied: queued as `auto` rows, offers recorded, finished volumes deleted. */
        suspend fun reconcile(seriesIds: Set<String>?): AutoApplied {
            var queued = 0
            var deleted = 0
            for (seriesId in seriesIds ?: db.auto().policies().map { it.seriesId }) {
                val input = readAutoInput(db, seriesId, { isPinned(it) }, { readingHeld() }) ?: continue
                val plan = autoPlan(input)
                for (id in plan.queue) step(id, Event.Enqueue(NewDownload(id, seriesId, RequestedBy.AUTO)))
                for (id in plan.delete) step(id, Event.Delete)
                if (plan.offer.isNotEmpty()) db.auto().insertOffers(plan.offer.map { AutoOfferEntity(seriesId, it) })
                queued += plan.queue.size
                deleted += plan.delete.size
            }
            committing()
            return AutoApplied(queued, deleted)
        }

        /** After a user's request for [id]: `keep` is set when the volume's acknowledged status is completed. */
        suspend fun keepIfCompleted(id: String) {
            if (db.ackedStatus(id) == COMPLETED) step(id, Event.Keep)
        }

        /** In a transaction: [event] on [id]'s row as it is now. Returns the row after. */
        suspend fun step(id: String, event: Event): DownloadEntity? = step(id, event, dao.get(id))

        /** In a transaction: [event] on [row], [id]'s row read in it. */
        suspend fun step(id: String, event: Event, row: DownloadEntity?): DownloadEntity? =
            when (val change = transition(row, event, clock(), ::freshId)) {
                Change.None -> row
                is Change.Write -> {
                    // A Retry of a queued row stays queued, but cuts the backoff short.
                    if (change.row.state == DownloadState.QUEUED && (row?.state != DownloadState.QUEUED || event is Event.Retry)) queued = true
                    if (row == null) dao.insert(listOf(change.row)) else dao.update(change.row)
                    drop(row, change.row)
                    after[id] = change.row
                    change.row
                }
                Change.Delete -> {
                    removed += row?.seriesId ?: id
                    dao.delete(listOf(id))
                    drop(row, null)
                    after[id] = null
                    null
                }
            }

        private fun drop(old: DownloadEntity?, new: DownloadEntity?) {
            old ?: return
            val kept = setOfNotNull(new?.copyId, new?.transferId)
            old.copyId?.takeIf { it !in kept }?.let { dropped[it] = old.copyBytes }
            old.transferId?.takeIf { it !in kept }?.let { dropped[it] = old.bytesDone }
        }

        /**
         * [dirId] goes to `.trash/` when no row references it, no reader pins it and no run writes
         * it; otherwise a dropped one ([knownSize]) is counted as detached until then.
         */
        suspend fun collect(dirId: String, knownSize: Long? = null) {
            val dir = File(root, dirId)
            val free = !dao.references(dirId) && !Pins.pinned(PinKey(accountDir, dirId)) && live.values.none { it.dirId == dirId }
            if (!free) {
                knownSize?.let { detach(dirId, it) }
                return
            }
            if (!dir.exists()) {
                // In the trash it stays counted until its wipe.
                if (!File(trash, dirId).exists()) undetach(dirId)
                return
            }
            detach(dirId, detachedSize(dirId) ?: knownSize ?: sizeOf(dir))
            if (dir.renameTo(File(trash, dirId))) wipes.trySend(Unit) else Log.e(TAG, "Couldn't move $dir to the trash")
        }
    }

    /** Before readiness, under the lock: the trash wiped, rows recovered with every copy validated, leftovers collected. */
    private suspend fun start() = withContext(Dispatchers.IO) {
        if (closed) return@withContext
        trash.mkdirs()
        trash.listFiles()?.forEach { if (!wipe(it)) detach(it.name, sizeOf(it)) }
        try {
            Locked().commit {
                recovering()
                for (row in dao.all()) step(row.contentId, Event.Recover(validCopy(row)), row)
                // Content fetched before this start that nothing owns: what an interrupted queueing left. Later rows may be a queueing in flight.
                val skip = reservedNow()
                // Optional and idempotent: with many reservations (a bound list) it waits for the next start.
                if (skip.size <= 400) db.content().pruneOlderThan(startedAt, skip.toList()).also { db.pruneSnapshots() } else Log.i(TAG, "Skipped the startup prune: ${skip.size} series reserved")
            }
        } catch (e: StorageFullException) {
            // The rows stay as stored: a running one is requeued at its claim, and a copy found incomplete stays
            // referenced until a start whose commit succeeds (opening it checks its files). Every volume still opens.
            Log.w(TAG, "Couldn't record the recovery of $accountDir: storage is full", e)
        }
        val referenced = dao.all().flatMap { listOfNotNull(it.copyId, it.transferId) }.toSet()
        root.listFiles()?.forEach { dir ->
            if (dir.name == TRASH || dir.name in referenced) return@forEach
            detach(dir.name, sizeOf(dir))
            if (!Pins.pinned(PinKey(accountDir, dir.name)) && !dir.renameTo(File(trash, dir.name))) Log.e(TAG, "Couldn't move $dir to the trash")
        }
    }

    /** A copy is whole: its directory, a manifest of its version, and every page. */
    private fun validCopy(row: DownloadEntity): Boolean {
        val dir = File(root, row.copyId ?: return true)
        val count = row.copyPageCount ?: return false
        val manifest = try {
            AppJson.decodeFromString(OfflineManifest.serializer(), File(dir, MANIFEST).readText())
        } catch (e: Exception) {
            return false
        }
        return manifest.version == row.copyVersion && (0 until count).all { File(dir, "$it").isFile }
    }

    private fun wipeTrash() {
        trash.listFiles()?.forEach { if (wipe(it)) undetach(it.name) }
    }

    private fun freshId(): String = generateSequence { newId() }.first { !File(root, it).exists() && !File(trash, it).exists() }

    private fun detach(id: String, size: Long) = synchronized(detached) {
        detached[id] = size
        _detachedBytes.value = detached.values.sum()
    }

    private fun undetach(id: String) = synchronized(detached) {
        if (detached.remove(id) != null) _detachedBytes.value = detached.values.sum()
    }

    private fun detachedSize(id: String) = synchronized(detached) { detached[id] }

    private fun sizeOf(dir: File) = dir.walk().filter { it.isFile }.sumOf { it.length() }

    private companion object {
        const val TAG = "DownloadStore"
        const val TRASH = ".trash"
    }
}

/** One claimed download as its producer sees it: it writes [dir] only, and reports through the store. */
class Run internal constructor(
    val account: String,
    val contentId: String,
    val seriesId: String,
    internal val token: Long,
    val dir: File,
    val version: String?,
    val pageCount: Int?,
    val pagesDone: Int,
    val bytesDone: Long,
    val fileSize: Long?,
    /** The file's size as last listed, before the run has a manifest. */
    val cachedFileSize: Long?,
)

/** What [DownloadStore.reconcileAuto] and [DownloadStore.setPolicy] changed: volumes queued and deleted. */
data class AutoApplied(val queued: Int, val deleted: Int)

/** A bulk download's result: [count] volumes queued, or, with [tooMany], offered and not queued. */
data class BulkQueued(val count: Int, val tooMany: Boolean)

/** A run's end: [result] is null when it was revoked, cancelled or threw; [row] is the row after. */
data class Ran<T>(val result: T?, val row: DownloadEntity?)

/** The end of a run couldn't be recorded, or the queue couldn't be repaired and the head claimed (before its producer exists). */
class DownloadsPersistenceFailed(cause: Throwable) : Exception(cause)
