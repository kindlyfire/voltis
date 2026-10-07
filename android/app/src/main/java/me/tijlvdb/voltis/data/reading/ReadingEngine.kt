package me.tijlvdb.voltis.data.reading

import java.io.IOException
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicInteger
import kotlin.coroutines.CoroutineContext
import kotlin.math.roundToInt
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineExceptionHandler
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.completeWith
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.doubleOrNull
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.net.Missing
import me.tijlvdb.voltis.domain.comic.pageFor
import me.tijlvdb.voltis.domain.catalog.isSeries
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.reading.AttentionItem
import me.tijlvdb.voltis.domain.reading.pageCount
import me.tijlvdb.voltis.domain.reading.Change
import me.tijlvdb.voltis.domain.reading.ComicAdapter
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.reading.ConflictKind
import me.tijlvdb.voltis.domain.reading.DrainResult
import me.tijlvdb.voltis.domain.reading.EmptyProgress
import me.tijlvdb.voltis.domain.reading.Envelope
import me.tijlvdb.voltis.domain.reading.Lane
import me.tijlvdb.voltis.domain.reading.LaneView
import me.tijlvdb.voltis.domain.reading.Op
import me.tijlvdb.voltis.domain.reading.OpKind
import me.tijlvdb.voltis.domain.reading.Outcome
import me.tijlvdb.voltis.domain.reading.PositionLabel
import me.tijlvdb.voltis.domain.reading.PromptChoice
import me.tijlvdb.voltis.domain.reading.ReaderAdapter
import me.tijlvdb.voltis.domain.reading.ReaderNotice
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.ReadingResult
import me.tijlvdb.voltis.domain.reading.ReadingSession
import me.tijlvdb.voltis.domain.reading.ReadingSnapshot
import me.tijlvdb.voltis.domain.reading.ReadingState
import me.tijlvdb.voltis.domain.reading.ReadingStore
import me.tijlvdb.voltis.domain.reading.ReadingTransport
import me.tijlvdb.voltis.domain.reading.ReviewOutcome
import me.tijlvdb.voltis.domain.reading.ReviewSession
import me.tijlvdb.voltis.domain.reading.SeriesAction
import me.tijlvdb.voltis.domain.reading.SeriesInfo
import me.tijlvdb.voltis.domain.reading.SeriesPrevious
import me.tijlvdb.voltis.domain.reading.SeriesReceipt
import me.tijlvdb.voltis.domain.reading.SeriesVolumes
import me.tijlvdb.voltis.domain.reading.Shown
import me.tijlvdb.voltis.domain.reading.Snapshot
import me.tijlvdb.voltis.domain.reading.SyncAction
import me.tijlvdb.voltis.domain.reading.SyncCommand
import me.tijlvdb.voltis.domain.reading.SyncNotice
import me.tijlvdb.voltis.domain.reading.SyncPrompt
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.reading.Undoable
import me.tijlvdb.voltis.domain.reading.covered
import me.tijlvdb.voltis.domain.reading.readingState
import me.tijlvdb.voltis.domain.reading.snapshots
import me.tijlvdb.voltis.domain.reading.guardMoved
import me.tijlvdb.voltis.domain.reading.guardOf
import me.tijlvdb.voltis.domain.reading.guardVolumes
import me.tijlvdb.voltis.domain.reading.project
import me.tijlvdb.voltis.domain.reading.replaces
import me.tijlvdb.voltis.domain.reading.titled
import me.tijlvdb.voltis.domain.sync.NoticeDetail
import me.tijlvdb.voltis.domain.sync.NoticeKind
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.domain.sync.StoredNotice

/**
 * The port of `createReadingActor` (frontend/src/pages/read/readingSync.ts) onto persisted lanes and
 * an outbox (P2 §5). Functions keep the web's names, so the two read side by side.
 *
 * One coroutine takes events from [inbox] in order: reader calls, answers, timers, prompt answers.
 * What an event changes is committed through [store] at its end, and before a request leaves; what
 * it does outside the engine (cache events, notices, answers to callers) waits for that commit,
 * unless it announces nothing stored ([effect]). A refused commit (a full disk) keeps the whole batch
 * and releases only those. Requests go out one at a time, outside the loop; their answers come back as events.
 */
class ReadingEngine(
    private val store: ReadingStore,
    private val transport: ReadingTransport,
    private val writer: ReadingWriter,
    private val events: CatalogEvents,
    private val connectivity: Connectivity,
    private val scheduler: SyncScheduler,
    parent: CoroutineScope,
    /** Where `restore` is called: the main thread. */
    private val main: CoroutineContext,
    private val clock: () -> Long = System::currentTimeMillis,
    /** An event or request that failed in a way the engine can't answer: a bug, or the store. */
    private val onError: (Throwable) -> Unit,
    /** An Error ended the engine: by default it goes to the thread's uncaught handler, which ends the process. */
    private val crash: (Throwable) -> Unit = { Thread.getDefaultUncaughtExceptionHandler()?.uncaughtException(Thread.currentThread(), it) },
    /**
     * The server answered 404 for an item: its download is marked gone. Called once the drop is
     * committed, with when ([clock]) the request that saw it was sent.
     */
    private val onGone: (contentId: String, at: Long) -> Unit = { _, _ -> },
    /** The account directory's name: the key of [ReadingHolds]. */
    private val accountDir: String = "",
) {
    private val job = SupervisorJob(parent.coroutineContext[Job])
    // Every Exception is caught per step ([guarded]), so only an Error gets here: it is fatal. The process crashes
    // and restarts from what is persisted, as for any Error, rather than running on with a dead engine.
    private val scope = CoroutineScope(
        parent.coroutineContext + job + CoroutineExceptionHandler { _, e ->
            onError(e)
            if (e !is Exception) crash(e)
        },
    )
    private val inbox = Channel<Event>(Channel.UNLIMITED)

    /** Why nothing is sent although something could be. */
    enum class Parked { STORAGE, UNREACHABLE, SIGNED_OUT }

    @Volatile private var started = false

    /** The event loop. When it ends, however it ends, the inbox is closed and every waiting caller fails. */
    private var loop: Job? = null

    /** Why the start failed; null once the outbox is loaded. */
    private var failure: Throwable? = IllegalStateException("Not started")
    private val parked = mutableSetOf<Parked>()

    /**
     * The writer's identity file refused its reservation ([StorageFullException]). A Room-only or empty commit proves
     * nothing about that file, so [Parked.STORAGE] stays until [STORAGE_RETRY] lets one more reservation attempt go.
     */
    private var writerRefused = false

    /** Parked only by held lanes' checks: nothing of the outbox waits on it ([settleDrains]). */
    private var parkedByChecks = false
    private var storageRetry: Job? = null
    private var probing: Job? = null

    /** Unreachable results since the last genuine answer: spaces the probes that unpark. */
    private var streak = 0

    /** `drain()` calls waiting for the pump to come to rest. */
    private val drains = mutableListOf<CompletableDeferred<DrainResult>>()

    private val lanes = mutableMapOf<String, LaneState>()

    /** Whether [ReadingHolds] was set by this engine and not cleared since. */
    private var holding = false

    /** Before any in-memory mutation: automatic deletes wait until what the engine holds is stored. */
    private fun hold() {
        if (holding) return
        holding = true
        ReadingHolds.set(accountDir, true)
    }

    private fun release() {
        holding = false
        ReadingHolds.set(accountDir, false)
    }

    /** What the next commit would write. */
    private fun unsaved() = learned.isNotEmpty() || dirtyLanes.isNotEmpty() || added.isNotEmpty() || changed.isNotEmpty() || deleted.isNotEmpty() || notices.isNotEmpty()

    /** IDs known to have no stored lane. */
    private val noLane = mutableSetOf<String>()
    private val queue = ArrayDeque<Task>()
    private val sessions = mutableSetOf<Session>()
    private var current: Task? = null
    private var dialog: Dialog? = null
    private var frozen = false
    private var timer: Job? = null

    /** What an Undo restores, by offer. In memory: an offer is only made to a reader that is there (P2 decision 9). */
    private val receipts = mutableMapOf<Int, Receipt>()
    private var nextReceipt = 0

    /** Offers made, and series asked about: once per process, as once per page on the web. */
    private val offered = mutableSetOf<String>()
    private val askedSeries = mutableSetOf<String>()

    /** The lanes the unsent ops showed something on at the last commit: the next one sets them right again. */
    private var projected = emptySet<String>()

    /** The shown status of every lane the unsent ops touch, as of the last commit; a series without a lane too. */
    private var shownStatuses = emptyMap<String, String?>()

    /** Writers other than [writer]'s current one whose requests are ours: those of stored unknown ops. */
    private val ownWriters = mutableSetOf<String>()

    // What the next commit writes, kept until it succeeds.
    /** Server states seen since the last commit, the newest of each content: imported by the primitive with the commit. */
    private val learned = linkedMapOf<String, ReadingSnapshot>()
    private val dirtyLanes = linkedSetOf<LaneState>()
    private val added = mutableListOf<Write>()
    private val changed = linkedSetOf<Write>()
    private val deleted = mutableListOf<Long>()
    private val notices = mutableListOf<StoredNotice>()

    /** What escapes the engine once the commit succeeds, in order; the [Effect.durable]-less ones also when it is refused. */
    private val effects = mutableListOf<Effect>()

    /**
     * [durable]: it announces something the commit stores (a command's `APPLIED` or `QUEUED`, a cache
     * event, a `Done` or an Undo offer), so it waits for a commit that succeeds. Anything else reports
     * the server, a failure or a question, and is released at once, also while the store refuses.
     */
    private class Effect(val durable: Boolean, val run: () -> Unit)

    /** Callers' results held in [effects]: [stop] fails them. */
    private val owed = mutableSetOf<CompletableDeferred<*>>()

    /** Every command's result, with its write, until the result is delivered: what a stop answers by. */
    private val results = ConcurrentHashMap<CompletableDeferred<CommandOutcome>, Write>()

    /** Callers waiting on an event: [stop] fails those whose event never ran. */
    private val pending: MutableSet<CompletableDeferred<*>> = ConcurrentHashMap.newKeySet()

    /** [reject] answers the caller of an event that can't run: the start failed. [restart] retries the start. */
    private class Event(val run: suspend () -> Unit, val reject: (Throwable) -> Unit = {}, val restart: Boolean = false)

    /** One lane: the stored row, and what the web keeps beside it in memory. */
    private class LaneState(var row: Lane) {
        val id get() = row.contentId

        /** Read on the main thread too, before a `restore`. */
        @Volatile var adapter: ReaderAdapter? = null
        var session: Session? = null

        /** The reader has placed itself: placements now go to it, not to where it opens. */
        var ready = false

        /** Reading waits for a check, or the answer to a conflict. */
        var held = row.needsReview

        /** A conflict waits to be asked about. Stored as `needs_review`, with an open conflict dialog. */
        var stale = row.needsReview

        /** Saved positions not yet fully refetched. */
        var dirty = false

        /** Its conflict dialog was dismissed: only Review or a new `load()` asks again. */
        var dismissed = false

        /** Opened from what is stored, unread: checked once the server answers again. */
        var unchecked = false

        /**
         * Its rebase read failed: the lane's ops wait until one is tried again, by a Retry, a drain or the
         * reader's next move. In memory only: a new engine starts with the rebase still stored.
         */
        var rebaseFailed = false
    }

    private class Receipt(val lane: LaneState, val previous: Snapshot, val series: SeriesPrevious?)

    private sealed class Task(val lane: LaneState) {
        /** When ([clock]) its latest request was sent. */
        var sentAt = 0L
    }

    /** A check, or with [done] a reader's initial read; with [rebase], adopted without comparing. [quick]: short timeouts. */
    private class Read(lane: LaneState, val done: CompletableDeferred<JsonObject>? = null, val rebase: Boolean = false, val quick: Boolean = false) : Task(lane) {
        /** The review whose outcome its answer decides. */
        var reviewer: Session? = null
    }

    /**
     * A series' volumes, as the server lists them: for a series command's guard. Never stored. [then] gets
     * the list; no list when the server wasn't reached; or why the request failed.
     */
    private class Volumes(lane: LaneState, val seriesId: String, val then: suspend (SeriesVolumes?, Exception?) -> Unit) : Task(lane)

    /** A stored op. [done] is its waiting caller; null once answered `QUEUED`, and for ops read back from the store. */
    private class Write(
        lane: LaneState,
        var op: Op,
        var done: CompletableDeferred<CommandOutcome>? = null,
        /** The bulk batch that made it; in memory only, never stored. */
        val origin: Long? = null,
    ) : Task(lane) {
        /** An undo's offer, while this process still has it: its failure is the reader's to retry. */
        var receipt: Int? = null

        /** Replaced while out: its row is gone, and it is never sent again. */
        var superseded = false

        /** The payload as the store has it; null for an op never stored. A different one is reading only this process has. */
        var storedPayload: JsonObject? = null

        val unsaved get() = !gone && (op.id == 0L || op.payload != storedPayload)

        /** Its row is deleted, or never will be written. */
        var gone = false

        /** Left by a server error with nobody watching: not sent again before the next drain. */
        var drainWait = false

        /** Sent again as the blocker of a sibling: if it fails again, the blocked lanes fail with it. */
        var retried = false

        /** The caller, while it still waits. */
        val caller get() = done?.takeIf { it.isActive }
        val reading get() = op.kind == OpKind.POSITION || op.kind == OpKind.FINISH
    }

    /** The one dialog slot. [review] is the lane a conflict asks about, kept `needs_review` while it is open. */
    private class Dialog(val session: Session, var review: LaneState?, var onAnswer: suspend (PromptChoice?) -> Unit) {
        var asked = 0
        val closed = CompletableDeferred<Unit>()
    }

    /**
     * Loads the stored lanes and outbox, then takes events. Nothing else runs before the outbox is
     * loaded; if that fails, every event's caller fails until [retryStart] loads it. Once.
     */
    fun start() {
        check(!started) { "Started already" }
        started = true
        loop = scope.launch {
            guarded { initialize() }
            for (event in inbox) {
                if (event.restart) {
                    if (failure != null) guarded { initialize() }
                    continue
                }
                failure?.let {
                    event.reject(it)
                    continue
                }
                guarded { event.run() }
                // Its own guard: what an event left uncommitted is tried again by the next one.
                guarded {
                    settleCallers()
                    // Also a refused commit: what escapes has been released, and the lanes show what memory holds.
                    val stored = commit()
                    publish()
                    settleDrains()
                    // Also an event that changed nothing, or whose change was reversed; not while a refused batch is kept.
                    if (stored && !unsaved()) release()
                }
            }
        }.also { it.invokeOnCompletion { shutDown() } }
        // Another request's genuine answer, or a probe that passed, unparks.
        scope.launch { connectivity.online.collect { if (it) post { reconnected() } } }
    }

    /** An unexpected failure of one step of the loop is reported, and the loop goes on. */
    private suspend fun guarded(block: suspend () -> Unit) {
        try {
            block()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            onError(e)
        }
    }

    /** After the loop: nothing can run an event, so its caller and every waiting result fail. */
    private fun shutDown() {
        release()
        inbox.close()
        while (true) inbox.tryReceive().getOrNull()?.reject(stopped()) ?: break
        listOfNotNull(current).plus(queue).forEach(::abandon)
        // Nothing commits now: what is stored stays for the account's next engine.
        settleResults()
        for (result in pending + owed.toList()) result.completeExceptionally(stopped())
        for (session in sessions.toList()) session.ending()
    }

    /** Tries the start again after it failed. */
    fun retryStart() {
        inbox.trySend(Event({}, restart = true))
    }

    private suspend fun initialize() {
        try {
            lanes.clear()
            noLane.clear()
            queue.clear()
            try {
                store.prune(clock() - PRUNE_AFTER)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                onError(e)
            }
            val stored = store.load()
            for (row in stored.lanes.values) lanes[row.contentId] = LaneState(row)
            for (op in stored.ops) {
                val task = Write(laneOf(op.contentId), op).also { it.storedPayload = op.payload }
                if (op.kind == OpKind.POSITION) task.set { copy(sealed = true) }
                op.sentWriter?.let(ownWriters::add)
                queue.addLast(task)
            }
            // The start is a drain pass: every stored op is tried once. No lane has a reader yet.
            for (lane in lanes.values) if (lane.row.failed) lane.set { copy(failed = false) }
            failure = null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            onError(e)
            failure = e
            lanes.clear()
            queue.clear()
            return
        }
        val stored = commit()
        publish()
        pump()
        if (stored && !unsaved()) release()
    }

    /** The web's `freeze()`: waiting callers fail, a response that comes later is ignored, and the outbox stays stored. Returns once the engine has stopped. */
    suspend fun stop() = withContext(NonCancellable) {
        try {
            val halted = CompletableDeferred<Unit>()
            val event = Event(
                {
                    // However it ends: stop() waits on it under the account switch's lock.
                    try {
                        freeze()
                        commit()
                        settleResults()
                    } finally {
                        halted.complete(Unit)
                    }
                },
                reject = { halted.complete(Unit) },
            )
            // The loop may have ended already (its parent cancelled): then the event is rejected, or can't be sent.
            loop?.invokeOnCompletion { halted.complete(Unit) }
            if (started && inbox.trySend(event).isSuccess) halted.await()
        } finally {
            inbox.close()
            try {
                job.cancelAndJoin()
            } finally {
                release()
                settleResults()
                for (result in pending + owed) result.completeExceptionally(stopped())
            }
        }
    }

    /** Writes' callers are answered once the stop has committed ([settleResults]). */
    private suspend fun freeze() {
        frozen = true
        timer?.cancel()
        storageRetry?.cancel()
        // A prompt whose answer fails doesn't keep the rest from stopping.
        dialog?.let { guarded { promptAnswered(it.session, null) } }
        listOfNotNull(current).plus(queue).forEach(::abandon)
        queue.clear()
        for (session in sessions) {
            session.prompt.value = null
            session.noticeChannel.close()
            session.ending()
        }
    }

    // Events.

    private fun post(block: suspend () -> Unit) {
        inbox.trySend(Event(block))
    }

    /** Runs [block] as an event; its result reaches the caller once the event is committed. */
    private suspend fun <T> actor(block: suspend () -> T): T {
        val result = CompletableDeferred<T>()
        pending += result
        result.invokeOnCompletion { pending -= result }
        val event = Event(
            {
                val outcome = try {
                    Result.success(block())
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    Result.failure(e)
                }
                answer(result, outcome)
            },
            reject = { result.completeExceptionally(it) },
        )
        if (inbox.trySend(event).isFailure) result.completeExceptionally(stopped())
        return result.await()
    }

    private fun effect(durable: Boolean = true, block: () -> Unit) {
        effects += Effect(durable, block)
    }

    /**
     * Completes [result] once the event is committed. A command's success waits for it, as it says
     * something is stored; a failure, `UNSAVED` and what other callers ask for ([load], [seed]) don't.
     */
    private fun <T> answer(result: CompletableDeferred<T>, outcome: Result<T>) {
        owed += result
        val stored = outcome.getOrNull().let { it is CommandOutcome && it != CommandOutcome.UNSAVED }
        effect(stored) {
            owed -= result
            result.completeWith(outcome)
        }
    }

    /**
     * Writes what the events changed and then releases their effects. When the store fails, the batch
     * is kept whole: the pump parks for storage, and the batch is tried again at the end of every event,
     * before a decision that queues an op ([storageOk]) and after [STORAGE_RETRY]. A commit that
     * succeeds un-parks it. Only effects that announce nothing stored are released meanwhile.
     */
    private suspend fun commit(): Boolean {
        for (lane in lanes.values) {
            val review = lane.stale || dialog?.review === lane
            if (lane.row.needsReview != review) lane.set { copy(needsReview = review) }
        }
        // For display only: a failure here never keeps an op from being stored or sent.
        val shown = try {
            projection()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            onError(e)
            shownStatuses = emptyMap()
            emptyMap()
        }
        // `shown_*` is written with what changes it: an op queued or answered, or a new acknowledged state.
        for (id in projected + shown.keys + dirtyLanes.map { it.id }) {
            val lane = lanes[id] ?: continue
            val s = shown[id] ?: lane.row.state.let { Snapshot(it.status, it.progress, it.lastReadAt) }
            // Progress as stored: SQL tells a projection by its text, and equal JSON in another key order would read as projected.
            if (lane.row.shownStatus != s.status || lane.row.shownProgress.toString() != s.progress.toString() || lane.row.shownLastReadAt != s.lastReadAt) {
                lane.set { copy(shownStatus = s.status, shownProgress = s.progress, shownLastReadAt = s.lastReadAt) }
            }
        }
        if (unsaved()) {
            val now = clock()
            for (lane in dirtyLanes) lane.row = lane.row.copy(touchedAt = now)
            val adds = added.toList()
            val change = Change(
                snapshots = learned.values.toList(), lanes = dirtyLanes.map { it.row }, ops = changed.map { it.op } + adds.map { it.op }, deleteOps = deleted.toList(), notices = notices.toList(),
            )
            val ids = try {
                store.commit(change)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                return storageFailed(e)
            }
            val saved = changed.toList()
            learned.clear()
            dirtyLanes.clear()
            added.clear()
            changed.clear()
            deleted.clear()
            notices.clear()
            ids.forEachIndexed { i, id -> adds[i].op = adds[i].op.copy(id = id) }
            for (task in saved + adds) task.storedPayload = task.op.payload
        }
        projected = shown.keys
        val released = effects.toList()
        effects.clear()
        runAll(released)
        if (!writerRefused && parked.remove(Parked.STORAGE)) {
            if (parked.isEmpty()) parkedByChecks = false
            pump()
        }
        return true
    }

    /** Each in order, each on its own: one that throws is reported and the rest still run. */
    private fun runAll(effects: List<Effect>) {
        for (effect in effects) {
            try {
                effect.run()
            } catch (e: Exception) {
                onError(e)
            }
        }
    }

    private fun storageFailed(e: Exception): Boolean {
        if (Parked.STORAGE !in parked) onError(e)
        parked += Parked.STORAGE
        if (storageRetry == null) {
            storageRetry = scope.launch {
                delay(STORAGE_RETRY)
                post {
                    storageRetry = null
                    writerRefused = false
                }
            }
        }
        // A command that raced the first refusal: kept in memory, with what it displaced, and told so. Not a rollback.
        for ((done, task) in results.toList()) {
            if (task.gone || task.op.id != 0L || task !in added || done.isCompleted) continue
            answer(done, Result.success(CommandOutcome.UNSAVED))
            task.done = null
        }
        val escaping = effects.filter { !it.durable }
        effects.removeAll(escaping)
        runAll(escaping)
        return false
    }

    /**
     * Whether a decision that queues an op may go on. While storage is known full the batch is tried first,
     * so the first command after space is back succeeds without waiting for a page turn; a decision made
     * while it is still refused fails before it changes anything.
     */
    private suspend fun storageOk(): Boolean = Parked.STORAGE !in parked || commit()

    /** What the unsent ops show (P2 §5, Projection), by lane: the one out first, then the queue. */
    private suspend fun projection(): Map<String, Snapshot> {
        val ops = (listOfNotNull(current) + queue).filterIsInstance<Write>().filter { !it.gone }
        shownStatuses = emptyMap()
        if (ops.isEmpty()) return emptyMap()
        val rows = mutableMapOf<String, Lane>()
        suspend fun add(id: String?) {
            if (id == null || id in rows) return
            // A lane that can't be read now is shown right by a later commit.
            val lane = try {
                existingLane(id)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                null
            }
            lane?.let { rows[id] = it.row }
        }
        for (task in ops) {
            add(task.lane.id)
            add(task.lane.row.parentId)
            add(task.op.payload.string("series_id"))
            task.op.guard?.guardVolumes()?.forEach { add(it) }
        }
        // A series without a lane starts from what its volume's lane last read of it, so its reader still shows the change.
        for (task in ops) task.lane.row.series?.let { if (it.id !in rows) rows[it.id] = Lane(it.id, state = ReadingState(it.revision, it.status)) }
        return project(ops.map { it.op }, rows).also { shown ->
            // Not compared with the stored series lane: the reader's copy of the series may differ from it.
            shownStatuses = shown.mapValues { it.value.status }
        }
    }

    private fun publish() {
        for (session in sessions) {
            val lane = lanes[session.contentId] ?: continue
            val row = lane.row
            session.view.value = LaneView(
                acked = row.state.takeIf { row.acked },
                status = row.shownStatus,
                // What the unsent ops do to the series shows too: Resume series and the end card follow it.
                series = row.series?.let { s -> if (s.id in shownStatuses) s.copy(status = shownStatuses[s.id]) else s },
                failures = row.failures,
                stale = lane.stale,
                tracking = row.tracking,
                unsent = (current as? Write)?.lane === lane || queue.any { it is Write && it.lane === lane },
                storageFull = Parked.STORAGE in parked && (listOfNotNull(current) + queue).any { it is Write && it.lane === lane && it.unsaved },
                parked = serverParked,
            )
        }
    }

    private fun LaneState.set(change: Lane.() -> Lane) {
        hold()
        row = row.change()
        dirtyLanes += this
    }

    private fun Write.set(change: Op.() -> Op) {
        hold()
        op = op.change()
        if (!gone && op.id != 0L) changed += this
    }

    private fun remove(task: Write) {
        if (task.gone) return
        hold()
        task.gone = true
        if (task.op.id == 0L) {
            added -= task
        } else {
            changed -= task
            deleted += task.op.id
        }
    }

    /** The lane of [id], made (unacknowledged) when there is none. */
    private suspend fun laneOf(id: String): LaneState = existingLane(id) ?: LaneState(Lane(id, acked = false)).also {
        hold()
        lanes[id] = it
        noLane -= id
        dirtyLanes += it
    }

    /** A lane, only if one is stored. */
    private suspend fun existingLane(id: String): LaneState? {
        lanes[id]?.let { return it }
        if (id in noLane) return null
        return store.lane(id)?.let { row -> LaneState(row).also { lanes[id] = it } } ?: run {
            noLane += id
            null
        }
    }

    private fun isOurs(writerId: String?) = writerId != null && (writerId == writer.writerId || writerId in ownWriters)

    /** A reader has the lane: not a reviewer, which only asks. */
    private val LaneState.reader get() = adapter != null && session?.reviewer != true

    // The queue.

    /** The item's series, or the item itself without one: an item without a parent is its own series. */
    private val LaneState.parentOf get() = row.parentId ?: id

    private fun hasReading(lane: LaneState) = queue.any { it is Write && it.lane === lane && it.reading }

    private fun retains(lane: LaneState) = lane.stale || (current as? Write)?.lane === lane || queue.any { it is Write && it.lane === lane }

    /**
     * Where the item stands finished: at its latest unanswered reading if that is a finish, else at
     * the saved place of a completed item.
     */
    private fun finishedAt(lane: LaneState): JsonObject? {
        var last: Write? = null
        for (task in listOfNotNull(current) + queue) {
            if (task is Write && task.lane === lane && task.reading && !task.superseded) last = task
        }
        if (last != null) return if (last.op.kind == OpKind.FINISH) last.op.payload else null
        return lane.row.state.progress.takeIf { lane.row.acked && lane.row.state.status == ReadingStatus.COMPLETED }
    }

    /**
     * Commands wait, like reading, for a check or conflict to be answered; reads and undos don't. An
     * op whose result is unknown goes before anything else of its series (P2 decision 2). An op that
     * waits for the next drain doesn't go, nor does anything of its lane after it. With [blocking]
     * false, whether it could go but for an op of its series whose result is unknown.
     */
    private fun runnable(task: Task, blocking: Boolean = true): Boolean {
        if (task !is Write) return true
        if (task.drainWait) return false
        // The lane's ops wait for its rebase read to succeed, and a failed one waits to be tried again.
        if (task.lane.row.rebase && (task.lane.held || task.lane.rebaseFailed)) return false
        val unknown = task.op.sentSeq != null
        if (blocking && !unknown && blocked(task)) return false
        if (behindStalled(task)) return false
        if (task.op.kind == OpKind.UNDO) return true
        if (!unknown && task.lane.held) return false
        if (!task.reading) return true
        if (task.op.kind == OpKind.POSITION && !task.op.sealed) return false
        return !task.lane.row.failed
    }

    // `current` needn't be looked at: pump() sends nothing while it is set.
    private fun blocked(task: Write) = blockerOf(task) != null

    private fun blockerOf(task: Write) =
        queue.firstOrNull { it is Write && it !== task && it.op.sentSeq != null && it.lane.parentOf == task.lane.parentOf } as Write?

    /** Behind an op of its lane, or of a lane it comes `after`, that waits for a drain or is reading that failed. */
    private fun behindStalled(task: Write): Boolean {
        for (other in queue) {
            if (other === task) return false
            if (other !is Write || (other.lane !== task.lane && other.lane.id !in task.op.after)) continue
            if (other.drainWait || (other.reading && other.lane.row.failed)) return true
        }
        return false
    }

    /** A waiting caller's op that can't go now, nor once what is out has been answered. */
    private fun stuck(task: Write) = serverParked || writerRefused || task.drainWait || task.lane.stale ||
        (task.lane.row.rebase && (task.lane.held || task.lane.rebaseFailed)) || (task.op.sentSeq == null && blocked(task)) || behindStalled(task)

    /**
     * The first task that can go. A task held back only by a blocker whose last send failed (its lane
     * `failed`, or waiting for a drain) sends that blocker again instead.
     */
    private fun pick(): Task? {
        for (task in queue) {
            if (runnable(task)) return task
            if (task !is Write || task.op.sentSeq != null || task.lane.row.failed || !runnable(task, blocking = false)) continue
            val blocker = blockerOf(task) ?: continue
            if (blocker.drainWait || (blocker.reading && blocker.lane.row.failed)) return blocker
        }
        return null
    }

    private val serverParked get() = Parked.UNREACHABLE in parked || Parked.SIGNED_OUT in parked

    private fun pump() {
        if (current != null || frozen || dialog != null) return
        val task = when {
            parked.isEmpty() -> pick()
            // A reader's first read doubles as a probe: its lane has nothing stored to open from.
            parked == setOf(Parked.UNREACHABLE) -> queue.firstOrNull { it is Read && it.done != null && !it.lane.row.acked }
            // A full disk stops no read: a write couldn't go anyway, as its send mark would be refused.
            parked == setOf(Parked.STORAGE) -> queue.firstOrNull { it is Read || it is Volumes }
            else -> null
        } ?: return
        if (task is Write) {
            // Not runnable itself: a blocker sent again for a sibling.
            task.retried = !runnable(task)
            if (task.retried) task.drainWait = false
        }
        current = if (task is Write && task.lane.row.rebase) {
            Read(task.lane, rebase = true)
        } else {
            queue.remove(task)
            task
        }
        launch(current!!)
    }

    private fun next() {
        current = null
        pump()
    }

    private fun push(task: Task) {
        if (!(task is Write && task.op.kind == OpKind.POSITION)) seal()
        queue.addLast(task)
        if (serverParked && task is Write) {
            // Committed while parked: it can go later without the user.
            if (!task.lane.held) scheduler.schedule()
        }
        pump()
    }

    private fun newOp(lane: LaneState, kind: String, payload: JsonObject, after: List<String> = emptyList()) =
        Op(contentId = lane.id, kind = kind, payload = payload, after = after, createdAt = clock())

    /** Ends the run of positions: they go as soon as they can, and nothing merges into them. */
    private fun seal() {
        timer?.cancel()
        timer = null
        for (task in queue) if (task is Write && task.op.kind == OpKind.POSITION && !task.op.sealed) task.set { copy(sealed = true) }
    }

    private fun sendNow() {
        seal()
        pump()
    }

    private fun dropReading(lane: LaneState) {
        for (task in queue.toList()) {
            if (task is Write && task.lane === lane && task.reading) {
                queue.remove(task)
                remove(task)
            }
        }
        (current as? Write)?.takeIf { it.lane === lane && it.reading }?.let {
            it.superseded = true
            remove(it)
        }
        lane.set { copy(failed = false, failures = 0) }
    }

    // Receiving.

    /** A server state, to be imported with the next commit: the table keeps the greatest seq of each content, so what is old is harmless. */
    private fun learn(id: String, state: ReadingState) = learn(ReadingSnapshot(id, state))

    private fun learn(snapshot: ReadingSnapshot) {
        val known = learned[snapshot.contentId]
        if (known != null && known.state.seq >= snapshot.state.seq) return
        hold()
        learned[snapshot.contentId] = snapshot
    }

    /** Both states of an answer, before anything decides what to do with it. */
    private fun learn(lane: LaneState, env: Envelope) {
        learn(lane.id, env.state)
        env.series?.let { learn(it.id, it.state) }
    }

    private suspend fun adopt(lane: LaneState, env: Envelope) {
        learn(lane, env)
        val foreign = lane.row.acked && env.state.revision != lane.row.state.revision && !isOurs(env.writer)
        lane.set { copy(acked = true, state = env.state, series = env.series, foreignEpoch = if (foreign) foreignEpoch + 1 else foreignEpoch) }
        // Propagation, not the choice: the series' own lane follows when it can, and a lookup that fails must not interrupt the lane's.
        env.series?.let {
            try {
                adoptSeries(it)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                onError(e)
            }
        }
    }

    /** The series' own lane takes the series as the answer has it, unless it retains something. */
    private suspend fun adoptSeries(series: SeriesInfo) {
        val lane = existingLane(series.id) ?: return
        val state = lane.row.state
        if (!lane.row.acked || retains(lane) || series.seq <= state.seq) return
        val foreign = series.revision != state.revision && !isOurs(writerOf(series.revision))
        lane.set { copy(state = series.state, foreignEpoch = if (foreign) foreignEpoch + 1 else foreignEpoch) }
    }

    /** Every response's way in: the expected state is adopted, anything else compared. */
    private suspend fun receive(lane: LaneState, env: Envelope) {
        learn(lane, env)
        lane.set { copy(series = env.series) }
        if (!lane.row.acked || env.state.revision == lane.row.state.revision || isOurs(env.writer)) return adopt(lane, env)
        if (lane.adapter == null) {
            if (!hasReading(lane)) {
                // Nobody to ask and no reading to lose: the other device's state is taken, whatever the lane is (P2 decision 5).
                adopt(lane, env)
                lane.set { copy(here = env.state.progress) }
                lane.stale = false
                lane.held = false
                lane.dismissed = false
                return
            }
            if (samePosition(lane, env.state)) {
                // What the web adopts without asking: the reading goes on top of it.
                adopt(lane, env)
                lane.stale = false
                lane.held = false
                return
            }
            lane.stale = true
            lane.held = true
            return
        }
        if (dialog != null || lane.session?.onScreen != true) {
            lane.stale = true
            lane.held = true
            return
        }
        compare(lane, env)
    }

    /**
     * With no reader, `compare()`'s only case that needs no answer, judged with the lane's own page
     * count: neither cleared nor completed elsewhere, and at the same position. Without a count, never.
     */
    private fun samePosition(lane: LaneState, next: ReadingState): Boolean {
        val pages = lane.row.pageCount ?: return false
        val acked = lane.row.state
        if (isCleared(next) && !isCleared(acked)) return false
        if (next.status == ReadingStatus.COMPLETED && acked.status != ReadingStatus.COMPLETED) return false
        return ComicAdapter({ pages }).samePosition(next.progress, acked.progress)
    }

    private fun body(op: Op): JsonObject = when (op.kind) {
        OpKind.COMMAND -> if (op.guard == null) op.payload else JsonObject(op.payload + ("ids" to guardIds(op)))
        OpKind.POSITION, OpKind.FINISH -> JsonObject(mapOf("op" to JsonPrimitive(op.kind), "progress" to op.payload))
        OpKind.UNDO -> JsonObject(mapOf("op" to JsonPrimitive("restore"), "snapshot" to op.payload.getValue("snapshot"), "series" to (op.payload["series"] ?: JsonNull)))
        // `series-reading` has no base: the guard stands in for it.
        OpKind.SERIES -> JsonObject(op.payload - "series_id" + ("ids" to guardIds(op)))
        else -> error("Unknown op kind ${op.kind}")
    }

    /** The volumes the op was guarded on: the server's answer covers them wherever they live now. At most what it accepts. */
    private fun guardIds(op: Op) = JsonArray(op.guard?.guardVolumes().orEmpty().take(MAX_IDS).map(::JsonPrimitive))

    private fun launch(task: Task) {
        scope.launch {
            try {
                when (task) {
                    is Read -> runRead(task)
                    is Write -> runWrite(task)
                    is Volumes -> runVolumes(task)
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                if (frozen) return@launch
                onError(e)
                post { crashed(task, e) }
            }
        }
    }

    /** A task that threw unexpectedly: its caller gets the error, and a write not yet answered goes back as a failed send does. */
    private suspend fun crashed(task: Task, err: Exception) {
        if (frozen) return
        if (current === task) current = null
        when (task) {
            is Read, is Volumes -> failed(task, err)
            is Write -> if (!task.gone && task !in queue) failed(task, err) else task.done?.let { answer(it, Result.failure(err)) }
        }
        pump()
    }

    private suspend fun runVolumes(task: Volumes) {
        task.sentAt = clock()
        val volumes = try {
            transport.volumes(task.seriesId)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            return actor {
                if (!frozen) failed(task, e)
                next()
            }
        }
        actor {
            if (!frozen) {
                answered()
                volumes.snapshots(task.seriesId).forEach { learn(it) }
                task.then(volumes, null)
            }
            next()
        }
    }

    private suspend fun runRead(task: Read) {
        val lane = task.lane
        task.sentAt = clock()
        val env = try {
            transport.get(lane.id, task.quick)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            return actor {
                if (frozen) abandon(task) else failed(task, e)
                next()
            }
        }
        val wait = actor {
            if (frozen) {
                abandon(task)
                return@actor null
            }
            answered()
            lane.unchecked = false
            if (task.rebase) {
                adopt(lane, env)
                // `here` follows the base when there is no reading of the lane's own on top of it.
                lane.set { copy(rebase = false, here = if (hasReading(lane)) here else env.state.progress) }
                lane.rebaseFailed = false
                // What a failed rebase showed the reader goes with it; nothing of the lane was sent meanwhile.
                if (lane.row.failed) lane.set { copy(failed = false, failures = 0) }
                next()
                return@actor null
            }
            val retained = lane.row.acked && (hasReading(lane) || lane.stale)
            if (lane.rebaseFailed) {
                // An answer: a lane with a rebase still reads it first. What the failure showed goes, other save failures stay.
                lane.rebaseFailed = false
                if (lane.row.failed) lane.set { copy(failed = false, failures = 0) }
            }
            lane.held = false
            lane.stale = false
            if (task.done == null || retained) {
                receive(lane, env)
            } else {
                adopt(lane, env)
                lane.set { copy(here = env.state.progress) }
            }
            if (task.done == null) {
                task.reviewer?.let { it.reviewed(if (dialog?.session === it) ReviewOutcome.ASKED else ReviewOutcome.RESOLVED) }
                next()
                return@actor null
            }
            // The reader opens where reconciling leaves it.
            dialog?.closed ?: CompletableDeferred(Unit)
        } ?: return
        wait.await()
        actor {
            if (frozen) {
                abandon(task)
            } else {
                lane.ready = true
                answer(task.done!!, Result.success(lane.row.here ?: env.state.progress))
            }
            next()
        }
    }

    private suspend fun runWrite(task: Write) {
        val lane = task.lane
        val series = task.op.kind == OpKind.SERIES
        task.sentAt = clock()
        // The pre-send check of a guarded op: the series' volumes as they are now. Two requests, so a change between them isn't caught (P2 Risks).
        val guarded = task.op.guard?.let { if (series) task.op.payload.string("series_id") else lane.id }
        val now = guarded?.let { seriesId ->
            try {
                transport.volumes(seriesId)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                return actor {
                    if (frozen) abandon(task) else failed(task, e)
                    next()
                }
            }
        }
        var reused = false
        val request = actor {
            if (frozen || task.superseded) {
                if (frozen) abandon(task)
                next()
                return@actor null
            }
            if (now != null) {
                answered()
                // Every state it lists is the server's, whatever is decided about the op.
                now.snapshots(guarded!!).forEach { learn(it) }
            }
            if (movedSince(task, now)) {
                changedElsewhere(task)
                // What was read is what the server has: the volumes' lanes follow it.
                if (now != null) seedVolumes(guarded!!, now)
                next()
                return@actor null
            }
            reused = task.op.sentSeq != null
            // A repeat goes as the (writer, seq) pair it went as, whatever the install's identity is now.
            val (writerId, seq) = task.op.sentSeq?.let { (task.op.sentWriter ?: writer.writerId) to it } ?: try {
                writer.ensureReserved()
                writer.writerId to writer.nextSeq()
            } catch (e: StorageFullException) {
                // As a refused send mark: nothing was sent, it goes first again once the store takes writes, and its lane isn't failed.
                writerRefused = true
                storageFailed(e)
                current = null
                queue.addFirst(task)
                // Reads queued behind it still go while STORAGE is parked.
                pump()
                return@actor null
            } catch (e: IOException) {
                // Local storage, not the server: nothing was sent, and the op stays as a failed send leaves it.
                failed(task, e)
                next()
                return@actor null
            }
            ownWriters += writerId
            task.set { copy(sentSeq = seq, sentWriter = writerId) }
            if (!commit()) {
                // Not durable: it doesn't leave, and goes first again once the store takes it.
                current = null
                queue.addFirst(task)
                return@actor null
            }
            val ours = mapOf("writer_id" to JsonPrimitive(writerId), "seq" to JsonPrimitive(seq))
            val base = if (series) emptyMap() else mapOf("base_revision" to (lane.row.state.revision?.let(::JsonPrimitive) ?: JsonNull))
            JsonObject(body(task.op) + base + ours)
        } ?: return
        task.sentAt = clock()
        var res: ReadingResult? = null
        var receipt: SeriesReceipt? = null
        // A series op's read after its write: its failure is a failed check, never a failed write.
        var unread: Exception? = null
        var readAt = 0L
        val env = try {
            if (series) {
                // Then a read of the lane, as on the web. Killed in between, the repeat changes nothing and the read is made again.
                receipt = transport.seriesReading(task.op.payload.string("series_id")!!, request)
                readAt = clock()
                try {
                    transport.get(lane.id, false)
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    unread = e
                    null
                }
            } else {
                transport.post(lane.id, request).also { res = it }.envelope
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            return actor {
                if (frozen) abandon(task) else failed(task, e)
                next()
            }
        }
        actor {
            if (frozen) {
                abandon(task)
            } else {
                answered()
                remove(task)
                // The states the answer carries are stored with the op's retiring, whether or not the lane's own read follows.
                receipt?.snapshots?.forEach { learn(it) }
                res?.items?.forEach { learn(it.snapshot) }
                // `failed` too: a blocker sent again for a sibling goes with its lane still failed.
                if (task.reading) lane.set { copy(failures = 0, failed = false) }
                if (env != null) {
                    if (series) lane.unchecked = false
                    receive(lane, env)
                }
                if (task.op.kind == OpKind.UNDO) task.receipt?.let(receipts::remove)
                // The bulk caller still waits: these effects are its batch's. Answered QUEUED (done = null) or
                // cancelled: ordinary. Catalog effects precede the APPLIED answer, and QUEUED is answered after
                // its event's effects: CatalogEvents relies on both to keep a batch's changes before its end.
                val origin = task.origin.takeIf { task.caller != null }
                val written = res
                if (written != null) {
                    // After its commit, as its own event: a dialog opened now would hold the pump for a prompt nobody sees.
                    if (task.reading) effect { post { feedback(lane, written) } }
                    refetch(lane, written.outcome, reused, origin)
                }
                // A series op or series clear wrote volumes too: their lanes follow what the answer says of them.
                (if (series) task.op.payload.string("series_id") else lane.id.takeIf { task.op.guard != null })?.let { seriesId ->
                    if (series) effect { events.emit(CatalogChange.ReadingChanged(seriesId, seriesId), origin) }
                    val items = receipt?.items.orEmpty() + res?.items.orEmpty()
                    for (item in items) seed(item.id, item.snapshot.state) { it.copy(parentId = seriesId) }
                    receipt?.series?.let { s -> if (existingLane(s.id) != null) seed(s.id, s.snapshot.state) { it } }
                }
                // After the change events: a batch's end then follows its last command's events.
                task.done?.let { answer(it, Result.success(CommandOutcome.APPLIED)) }
                unread?.let {
                    // Checked again on the next genuine answer: nothing shows what the op did until then.
                    lane.unchecked = true
                    failed(Read(lane).also { read -> read.sentAt = readAt }, it)
                }
            }
            next()
        }
    }

    /** A request that didn't succeed, by what came back (P2 §5, Results). */
    private suspend fun failed(task: Task, err: Exception) {
        // A sign-out is genuine, but parks for good: nothing to unpark or check first.
        // A server error unparks only: a check it started would fail the same way.
        if (err is ReadingFailure && err !is ReadingFailure.Unreachable && err !is ReadingFailure.SignedOut) answered(check = err !is ReadingFailure.ServerError)
        when (err) {
            is ReadingFailure.Unreachable -> return parkFor(task, Parked.UNREACHABLE, err)
            is ReadingFailure.SignedOut -> return parkFor(task, Parked.SIGNED_OUT, err)
        }
        val lane = task.lane
        if (task is Volumes) return task.then(null, err)
        if (task is Read) {
            if (err is ReadingFailure.Gone) {
                // Nothing of the item is left to read or write: also a rebase read would wait for ever.
                goneLane(lane, err, null, task.sentAt)
                task.done?.let { answer(it, Result.failure(err)) }
                task.reviewer?.reviewed(ReviewOutcome.FAILED)
                return
            }
            // A server error on a reader's open: from what is stored, as when the server can't be reached.
            val done = task.done
            if (err is ReadingFailure.ServerError && done != null && lane.row.acked) return fallback(lane, done)
            // Refused: an answer about the lane, so it is checked.
            if (err is ReadingFailure && err !is ReadingFailure.ServerError) lane.unchecked = false
            lane.held = lane.stale
            if (task.rebase) rebaseFailed(lane)
            task.done?.let { answer(it, Result.failure(err)) }
            task.reviewer?.reviewed(ReviewOutcome.FAILED)
            return
        }
        task as Write
        val conflict = err as? ReadingFailure.Conflict
        if (task.reading && task.superseded) {
            if (conflict != null) receive(lane, conflict.current)
            return
        }
        // The server answered, so a repeat takes a new seq. Anything else may have landed, and keeps it.
        val known = conflict != null || err is ReadingFailure.Refused || err is ReadingFailure.Gone
        task.set {
            copy(sentSeq = sentSeq.takeUnless { known }, sentWriter = sentWriter.takeUnless { known }, attempts = attempts + 1, lastError = err.message)
        }
        if (conflict != null && isOurs(conflict.current.writer)) {
            // Our own write, whose acknowledgement was lost: this one goes again on top of it.
            adopt(lane, conflict.current)
            queue.addFirst(task)
            return
        }
        when {
            err is ReadingFailure.Refused -> drop(task, err, SyncNotice.Refused(titleOf(lane), err.message.orEmpty()))
            err is ReadingFailure.Gone -> gone(task, err)
            conflict != null && task.reading -> queue.addFirst(task)
            // Last writer wins: it goes again on the state receive() adopts, or is held with the lane's reading (P2 decision 4).
            conflict != null && plain(task.op) -> queue.addFirst(task)
            conflict != null -> changedElsewhere(task)
            else -> serverError(task, err)
        }
        if (conflict != null) receive(lane, conflict.current)
    }

    /**
     * A failed rebase read holds the lane's ops until it is tried again, rather than at once. A reader is told as
     * for a failed finish (Retry); with nobody there, the worker is asked, as for a server error.
     */
    private fun rebaseFailed(lane: LaneState) {
        lane.rebaseFailed = true
        lane.set { copy(failed = true, failures = if (lane.reader) maxOf(failures + 1, 3) else failures) }
        if (queue.any { it is Write && it.lane === lane }) scheduler.schedule()
    }

    /**
     * A server error, or one of our own. With its reader there, reading fails as on the web; a caller
     * still waiting gets the error. With nobody watching, the op is kept until the next drain.
     */
    private fun serverError(task: Write, err: Exception) {
        val lane = task.lane
        if (task.retried) {
            // Sent again for its siblings, and failed again: their readers see it too.
            task.retried = false
            for (other in queue.filterIsInstance<Write>().map { it.lane }.distinct()) {
                if (other !== lane && other.parentOf == lane.parentOf) other.set { copy(failed = true, failures = failures + 1) }
            }
        }
        val caller = task.caller
        when {
            task.reading && lane.reader -> {
                queue.addFirst(task)
                // Operations building on it would overtake it: those with a caller fail with it; the rest wait behind it.
                for (later in queue.toList()) {
                    if (later !is Write || (later.op.kind != OpKind.COMMAND && later.op.kind != OpKind.SERIES) || lane.id !in later.op.after) continue
                    val waiting = later.caller ?: continue
                    queue.remove(later)
                    remove(later)
                    answer(waiting, Result.failure(err))
                }
                // Reading on retries a position, but nothing retries a finish but Retry: offer it now.
                lane.set { copy(failed = true, failures = if (task.op.kind == OpKind.FINISH) maxOf(failures + 1, 3) else failures + 1) }
            }
            !task.reading && caller != null -> {
                remove(task)
                answer(caller, Result.failure(err))
            }
            watched(task) -> {
                remove(task)
                undoFailed(task, err)
            }
            else -> {
                task.drainWait = true
                queue.addFirst(task)
                scheduler.schedule()
            }
        }
    }

    /** Dropped: it can never succeed. Its caller gets [err], an undo's reader its Retry; with neither, [notice] is stored. */
    private suspend fun drop(task: Write, err: Exception, notice: SyncNotice.Lasting) {
        remove(task)
        val caller = task.caller
        when {
            caller != null -> answer(caller, Result.failure(err))
            watched(task) -> undoFailed(task, err)
            else -> storeNotice(task.lane, notice)
        }
    }

    /** An undo whose offer this process made, with its reader still there: the failure is a snackbar with Retry. */
    private fun watched(task: Write) = task.op.kind == OpKind.UNDO && task.receipt != null && task.lane.session != null

    /** A server error (or one of ours) offers Retry. A conflict, refusal or 404 can't succeed again: its offer goes. */
    private fun undoFailed(task: Write, err: Exception) {
        val receipt = task.receipt ?: return
        if (err is ReadingFailure && err !is ReadingFailure.ServerError) {
            receipts -= receipt
            return task.lane.notify(ReaderNotice(SyncNotice.Failed(SyncAction.UNDO, err)))
        }
        task.lane.notify(ReaderNotice(SyncNotice.Failed(SyncAction.UNDO, err)) { post { undo(receipt) } })
    }

    /** A status other than completed, or a series' status: it destroys nothing, so it is never dropped for a conflict. */
    private fun plain(op: Op) = op.kind == OpKind.COMMAND && (op.payload.string("op") == "series_status" || (op.payload.string("op") == "set_status" && !replaces(op.payload)))

    /**
     * Whether what [task] was made against moved since (P2 §5, Guards): a destructive command's or
     * an undo's lane adopted another device's state ([Op.epoch]), or the series or a covered volume
     * of a guarded op was written by someone else, as [now] lists them.
     */
    private fun movedSince(task: Write, now: SeriesVolumes?): Boolean {
        val op = task.op
        val epoch = op.epoch
        if (epoch != null && (op.kind == OpKind.UNDO || (op.kind == OpKind.COMMAND && replaces(op.payload))) && epoch != task.lane.row.foreignEpoch) return true
        val guard = op.guard ?: return false
        if (now == null) return false
        val covered = if (op.kind == OpKind.SERIES) {
            // An `until_id` no longer listed covers nothing here: the server's 404 drops the op.
            covered(now.volumes, op.payload.string("action")!!, op.payload["include_unread"] == JsonPrimitive(true), op.payload.string("until_id")).orEmpty()
        } else {
            now.volumes.map { it.id }
        }
        return guardMoved(guard, now, covered) { isOurs(writerOf(it)) }
    }

    /**
     * Dropped: what it was made against changed on another device. Its caller hears so, else a notice
     * is stored; it may have been applied when an earlier send's result is unknown.
     */
    private suspend fun changedElsewhere(task: Write) {
        val command = commandOf(task.op)
        val uncertain = task.op.sentSeq != null
        drop(task, ReadingFailure.ChangedElsewhere(command, uncertain), SyncNotice.ChangedElsewhere(titleOf(task.lane), command, uncertain))
    }

    /** A write's 404: a series op only drops itself (the missing thing is likely its `until_id`); any other drops its lane's ops. */
    private suspend fun gone(task: Write, err: Exception) {
        if (task.op.kind == OpKind.SERIES) return drop(task, err, SyncNotice.Refused(titleOf(task.lane), Missing.VOLUME))
        goneLane(task.lane, err, task, task.sentAt)
    }

    /**
     * The item is gone from the server (a write's or a read's 404): every op of its lane but series
     * ops is dropped, callers hear [err], and one notice is stored when any dropped op had nobody
     * waiting. Nothing is left to review. Once that is committed, its download is marked gone.
     */
    private suspend fun goneLane(lane: LaneState, err: Exception, first: Write?, sentAt: Long) {
        val dropped = queue.filterIsInstance<Write>().filter { it !== first && it.lane === lane && it.op.kind != OpKind.SERIES }
        queue.removeAll(dropped)
        var unwatched = false
        for (op in listOfNotNull(first) + dropped) {
            remove(op)
            op.caller?.let { answer(it, Result.failure(err)) } ?: run { unwatched = true }
        }
        if (unwatched) storeNotice(lane, SyncNotice.Gone(titleOf(lane)))
        lane.held = false
        lane.stale = false
        lane.dismissed = false
        lane.unchecked = false
        lane.set { copy(failed = false, failures = 0, rebase = false) }
        val id = lane.id
        effect { onGone(id, sentAt) }
    }

    /**
     * No answer, or signed out: the task stays first and nothing is counted. A reader's first read
     * opens from what is stored when there is something. Then the pump parks.
     */
    private suspend fun parkFor(task: Task, why: Parked, err: Exception) {
        val lane = task.lane
        val check = task is Read && task.done == null && !task.rebase && lane.adapter == null
        when (task) {
            is Volumes -> task.then(null, null)
            is Read -> if (task.done != null) {
                if (lane.row.acked) fallback(lane, task.done) else {
                    lane.held = lane.stale
                    answer(task.done, Result.failure(err))
                }
            } else if (!task.rebase) {
                lane.held = lane.stale
                lane.unchecked = true
                task.reviewer?.reviewed(ReviewOutcome.FAILED)
            }
            is Write -> if (!task.superseded) {
                task.retried = false
                task.set { copy(attempts = attempts + 1, lastError = "Unreachable") }
                queue.addFirst(task)
            }
        }
        park(why, check)
    }

    /** [check]: a held lane's check parks it, which leaves nothing of the outbox waiting. */
    private suspend fun park(why: Parked, check: Boolean = false) {
        parkedByChecks = check && (parked.isEmpty() || parkedByChecks)
        parked += why
        if (why == Parked.UNREACHABLE) {
            streak++
            probe()
        }
        // A reader's open waiting behind is answered from what is stored, and a guard made from what is cached.
        for (task in queue.toList()) {
            if (task is Read && task.done != null && task.lane.row.acked) {
                queue.remove(task)
                fallback(task.lane, task.done)
            } else if (task is Volumes) {
                queue.remove(task)
                task.then(null, null)
            }
        }
        if (queue.any { it is Write && !it.lane.held }) scheduler.schedule()
    }

    /** After a park: a probe that passes unparks. The second unreachable answer in a row on, it waits longer each time. */
    private fun probe() {
        if (probing?.isActive == true) return
        val wait = if (streak <= 1) 0L else minOf(PROBE_BACKOFF shl minOf(streak - 2, 4), PROBE_BACKOFF_MAX)
        probing = scope.launch {
            delay(wait)
            if (connectivity.probe()) post { reconnected() }
        }
    }

    /** A genuine answer from the server. */
    private fun answered(check: Boolean = true) {
        streak = 0
        connectivity.answered()
        reconnected(check)
    }

    /** The server can be reached again: unpark, and check the lanes opened from what was stored. */
    private fun reconnected(check: Boolean = true) {
        if (frozen) return
        parked -= Parked.UNREACHABLE
        if (parked.isEmpty()) parkedByChecks = false
        if (check) for (lane in lanes.values) {
            // The mark stays until a read of the lane is answered, other than with a server error.
            if (lane.unchecked && lane.adapter != null && !lane.dismissed) check(lane)
        }
        pump()
    }

    /** `load()` without the server: where the reader stands, or the acknowledged state; checked once the server answers. */
    private fun fallback(lane: LaneState, done: CompletableDeferred<JsonObject>) {
        lane.ready = true
        lane.held = lane.stale
        lane.unchecked = true
        answer(done, Result.success(lane.row.here ?: lane.row.state.progress))
    }

    private fun titleOf(lane: LaneState) = lane.row.title ?: lane.adapter?.content()?.title ?: lane.id

    private fun commandOf(op: Op) = when {
        op.kind == OpKind.UNDO -> SyncCommand.UNDO
        op.kind == OpKind.COMMAND && replaces(op.payload) && op.payload.string("op") != "clear" -> SyncCommand.MARK_COMPLETED
        else -> op.payload.string("op") ?: op.payload.string("action") ?: op.kind
    }

    /** Named with the item's series, when its title is known. */
    private suspend fun storeNotice(lane: LaneState, notice: SyncNotice.Lasting) {
        val (kind, detail) = when (notice) {
            is SyncNotice.ChangedElsewhere -> NoticeKind.CHANGED_ELSEWHERE to NoticeDetail(command = notice.command, uncertain = notice.uncertain)
            is SyncNotice.Refused -> NoticeKind.REFUSED to NoticeDetail(message = notice.message)
            is SyncNotice.Gone -> NoticeKind.GONE to NoticeDetail()
        }
        val series = lane.row.parentId?.let { id ->
            lanes[id]?.row?.title ?: try {
                store.title(id)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                null
            }
        }
        hold()
        notices += StoredNotice(0, lane.id, titled(series, notice.title), kind, detail, clock())
    }

    private fun refetch(lane: LaneState, outcome: String, reused: Boolean, origin: Long? = null) {
        val change: CatalogChange
        if (outcome == Outcome.NONE) {
            // A repeat answered "nothing to do" confirms a write that landed unseen.
            if (!reused) return
            change = CatalogChange.ReadingChanged(lane.id, lane.parentOf)
        } else if (outcome != Outcome.SAVED || lane.adapter == null) {
            lane.dirty = false
            change = CatalogChange.ReadingChanged(lane.id, lane.parentOf)
        } else {
            // While reading, the rest refetches when the reader leaves: not on every page.
            lane.dirty = true
            change = CatalogChange.PositionSaved(lane.id, lane.parentOf)
        }
        effect { events.emit(change, origin) }
    }

    /** Reading that went against a deliberate status gets an Undo; a held series asks once. Only for a reader that is there to see it. */
    private fun feedback(lane: LaneState, res: ReadingResult) {
        if (res.outcome !in listOf(Outcome.SAVED, Outcome.STARTED, Outcome.MOVED_TO_READING, Outcome.COMPLETED)) return
        val session = lane.session?.takeIf { lane.reader && it.onScreen } ?: return
        fun keep(previous: Snapshot): Int {
            receipts[++nextReceipt] = Receipt(lane, previous, res.seriesPrevious)
            return nextReceipt
        }
        val undoable = if (res.outcome == Outcome.COMPLETED) Undoable.MARKED_COMPLETED else Undoable.MOVED_TO_READING
        val series = res.series
        val status = series?.status
        if (series != null && (status == ReadingStatus.ON_HOLD || status == ReadingStatus.DROPPED) && dialog == null && askedSeries.add(series.id)) {
            val receipt = res.previous?.let(::keep)
            return openDialog(session, SyncPrompt.SeriesHeld(status, undoable.takeIf { receipt != null })) { choice ->
                // A dismissal is Keep.
                if (choice == PromptChoice.MOVE) moveSeries(lane) else if (choice == PromptChoice.UNDO && receipt != null) undo(receipt)
            }
        }
        // Starting a volume reopened the completed series.
        val reopened = res.seriesPrevious?.takeIf { it.status == ReadingStatus.COMPLETED }
        if (reopened != null && series != null && offered.add("reopen:${series.id}")) {
            lane.notify(ReaderNotice(SyncNotice.UndoOffer(Undoable.SERIES_MOVED_TO_READING)) { post { restoreSeries(lane, reopened) } })
        }
        val previous = res.previous ?: return
        if (!offered.add("${res.outcome}:${lane.id}")) return
        val receipt = keep(previous)
        lane.notify(ReaderNotice(SyncNotice.UndoOffer(undoable)) { post { undo(receipt) } })
    }

    /** The engine is stopping: a read's caller fails at once. Writes' callers are [settleResults]'. */
    private fun abandon(task: Task) {
        if (task is Read) task.done?.completeExceptionally(stopped())
    }

    /**
     * After the last commit: every command result not yet delivered is answered by its write's row. A
     * row with a database ID stays for the account's next engine, which sends it (or replays it,
     * answered `none`), so its caller hears `QUEUED`; an op never stored fails its caller. A
     * delivered result has left [results], so a deletion that was committed never counts as stored.
     */
    private fun settleResults() {
        for ((result, task) in results.toList()) {
            if (task.op.id != 0L) result.complete(CommandOutcome.QUEUED) else result.completeExceptionally(stopped())
        }
    }

    /** Callers whose op can't go now hear `QUEUED`, once it is committed: at the end of every event. */
    private fun settleCallers() {
        for (task in queue) {
            if (task !is Write) continue
            val caller = task.caller ?: continue
            if (!stuck(task)) continue
            answer(caller, Result.success(CommandOutcome.QUEUED))
            task.done = null
        }
    }

    /** Answers `drain()` once the pump has come to rest. Ops of held lanes don't count, unless a send's result is unknown. */
    private fun settleDrains() {
        if (drains.isEmpty() || current != null) return
        val waiting = queue.any { it is Write && (!it.lane.held || it.op.sentSeq != null) }
        val result = when {
            dialog != null -> DrainResult.BUSY
            (parked.isNotEmpty() && !(parked == setOf(Parked.UNREACHABLE) && parkedByChecks)) || waiting -> DrainResult.WAITING
            else -> DrainResult.DONE
        }
        if (result == DrainResult.WAITING && waiting) scheduler.schedule()
        drains.toList().also { drains.clear() }.forEach { it.complete(result) }
    }

    /** A pass over everything unwatched, as a Retry: waits for a drain end, unwatched lanes lose `failed`, and the pump unparks. */
    private fun drainPass() {
        for (task in queue) if (task is Write) task.drainWait = false
        for (lane in lanes.values.toList()) {
            // Also with a reader: a rebase read that failed is read again, so the failure it showed no longer gates the lane's reading.
            if (lane.rebaseFailed) {
                lane.rebaseFailed = false
                if (lane.row.failed) lane.set { copy(failed = false) }
            }
            if (lane.adapter != null) continue
            if (lane.row.failed) lane.set { copy(failed = false) }
            // Awaiting review with nobody there: read again. With nothing unsent it adopts; with reading, the same position does (receive()).
            if (lane.stale) check(lane)
        }
        reconnected()
    }

    // Commands.

    /**
     * Queues an operation after settling the unsaved reading of the items it writes: reading it
     * [replaces] is dropped; reading it comes `after` goes first, and if that fails, the operation
     * fails with it.
     */
    private fun enqueue(task: Write, replaces: List<String>) {
        for (id in replaces) lanes[id]?.let(::dropReading)
        for (id in task.op.after) lanes[id]?.set { copy(failed = false) }
        hold()
        added += task
        push(task)
    }

    /** [guard] and [covered] are a series clear's: the volumes it clears with the series, whose reading it replaces too. */
    private suspend fun command(
        lane: LaneState,
        request: JsonObject,
        done: CompletableDeferred<CommandOutcome> = result(),
        guard: JsonObject? = null,
        covered: List<String> = emptyList(),
        origin: Long? = null,
    ): CompletableDeferred<CommandOutcome> = done.also { queueCommand(lane, request, it, guard, covered, origin) }

    /**
     * Without [done], nobody waits for it: a failure is the no-caller case (P2 §5, Results). False when it
     * wasn't queued: stopped, or storage is full ([storageOk]), with nothing changed and [done] failed.
     */
    private suspend fun queueCommand(
        lane: LaneState,
        request: JsonObject,
        done: CompletableDeferred<CommandOutcome>?,
        guard: JsonObject? = null,
        covered: List<String> = emptyList(),
        origin: Long? = null,
    ): Boolean {
        if (frozen) {
            done?.completeExceptionally(stopped())
            return false
        }
        if (!storageOk()) {
            done?.let { answer(it, Result.failure(StorageFullException())) }
            return false
        }
        val replaces = replaces(request)
        // What the op was made against: a change elsewhere since drops a replacing one. Not when the
        // lane already says "Changed on another device": the user chose to replace (P2 decision 7).
        val epoch = lane.row.foreignEpoch.takeUnless { lane.stale && replaces }
        if (lane.stale && lane.adapter != null) {
            check(lane)
        } else if (lane.stale && replaces) {
            // A page's command, on a lane that waits to be asked about: the review ends, and the
            // server's state is read and adopted before the command goes.
            endReview(lane)
        }
        val ids = listOf(lane.id)
        val task = Write(lane, newOp(lane, OpKind.COMMAND, request, after = if (replaces) emptyList() else ids).copy(epoch = epoch, guard = guard), done, origin)
        done?.let { track(it, task) }
        enqueue(task, if (replaces) ids + covered else emptyList())
        return true
    }

    /** Nothing is left to ask about: the reading is replaced, and the server's state is read and adopted before the lane's next op. */
    private fun endReview(lane: LaneState) {
        lane.stale = false
        lane.held = false
        lane.dismissed = false
        lane.set { copy(rebase = true) }
    }

    /** A series op or series clear replaces the reading of the held volumes it covers: their reviews end, an open dialog about one too. */
    private suspend fun endReviews(covered: List<String>) {
        for (id in covered) {
            val lane = existingLane(id)?.takeIf { it.stale || dialog?.review === it } ?: continue
            dialog?.takeIf { it.review === lane }?.let { promptAnswered(it.session, null) }
            endReview(lane)
        }
    }

    /** A result nobody may be awaiting: it fails with the engine's stop all the same. */
    private fun result() = CompletableDeferred<CommandOutcome>().also { done ->
        pending += done
        done.invokeOnCompletion { pending -= done }
    }

    private fun track(done: CompletableDeferred<CommandOutcome>, task: Write) {
        results[done] = task
        done.invokeOnCompletion { results.remove(done) }
    }

    private fun seriesStatus(status: String?) = JsonObject(mapOf("op" to JsonPrimitive("series_status"), "status" to JsonPrimitive(status)))

    private suspend fun restoreSeries(lane: LaneState, series: SeriesPrevious) {
        val request = JsonObject(mapOf("op" to JsonPrimitive("series_status"), "series" to series.json()))
        val done = command(lane, request)
        done.invokeOnCompletion { err ->
            if (err != null) post { lane.notify(ReaderNotice(SyncNotice.Failed(SyncAction.UNDO, err)) { post { restoreSeries(lane, series) } }) }
            else if (done.unsaved()) post { lane.notify(ReaderNotice(SyncNotice.NotSaved)) }
        }
    }

    private suspend fun moveSeries(lane: LaneState) {
        val done = command(lane, seriesStatus(ReadingStatus.READING))
        done.invokeOnCompletion { err ->
            if (err != null) post { lane.notify(ReaderNotice(SyncNotice.Failed(SyncAction.MOVE_SERIES, err)) { post { moveSeries(lane) } }) }
            else if (done.unsaved()) post { lane.notify(ReaderNotice(SyncNotice.NotSaved)) }
        }
    }

    /** A completed, successful result that is [CommandOutcome.UNSAVED]: still queued in memory, so no Retry and no `Done`. */
    @OptIn(ExperimentalCoroutinesApi::class)
    private fun CompletableDeferred<CommandOutcome>.unsaved() = getCompleted() == CommandOutcome.UNSAVED

    /**
     * A series command (P2 §5, Guards): the series' volumes are asked for first, from the cached rows
     * when the server isn't reached, and seeded; [make] then queues the op with what it covers. With
     * neither, [done] fails with "Needs a connection" and nothing is queued.
     */
    private suspend fun withVolumes(lane: LaneState, seriesId: String, done: CompletableDeferred<CommandOutcome>, make: suspend (SeriesVolumes) -> Unit) {
        suspend fun resolve(fetched: SeriesVolumes?, err: Exception?) {
            // Stopped: [done] fails with the stop.
            if (frozen) return
            try {
                if (err != null) throw err
                // The answer of a write ahead of this one may have been the first refusal: nothing changes then.
                if (!storageOk()) throw StorageFullException()
                val volumes = fetched ?: store.volumes(seriesId) ?: throw SyncUnavailable.NeedsConnection()
                // From the server's list or the stored snapshots: a lane takes a state only if its seq is greater.
                seedVolumes(seriesId, volumes)
                make(volumes)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                answer(done, Result.failure(e))
            }
        }
        if (frozen) {
            done.completeExceptionally(stopped())
        } else if (!storageOk()) {
            answer(done, Result.failure(StorageFullException()))
        } else if (serverParked || !connectivity.online.value) {
            resolve(null, null)
        } else {
            push(Volumes(lane, seriesId, ::resolve))
        }
    }

    /**
     * `series-reading` for [seriesId], queued on [lane]: a reader's item, or the series itself. The
     * unsent reading of the volumes it covers is dropped; [lane]'s own, if not covered, goes first.
     */
    private suspend fun seriesOp(
        lane: LaneState,
        seriesId: String,
        action: String,
        includeUnread: Boolean,
        untilId: String?,
        done: CompletableDeferred<CommandOutcome>,
        origin: Long? = null,
    ) =
        withVolumes(lane, seriesId, done) { volumes ->
            val covered = covered(volumes.volumes, action, includeUnread, untilId) ?: throw ReadingFailure.Refused(404, Missing.VOLUME)
            val payload = buildMap<String, JsonElement> {
                put("series_id", JsonPrimitive(seriesId))
                put("action", JsonPrimitive(action))
                if (action == SeriesAction.MARK_SERIES_COMPLETED) put("include_unread", JsonPrimitive(includeUnread))
                untilId?.let { put("until_id", JsonPrimitive(it)) }
            }
            val after = if (lane.id in covered) emptyList() else listOf(lane.id)
            endReviews(covered)
            val task = Write(lane, newOp(lane, OpKind.SERIES, JsonObject(payload), after).copy(guard = guardOf(volumes, covered)), done, origin)
            track(done, task)
            enqueue(task, covered)
        }

    private suspend fun completeSeries(lane: LaneState, seriesId: String, includeUnread: Boolean) {
        val done = result()
        seriesOp(lane, seriesId, SeriesAction.MARK_SERIES_COMPLETED, includeUnread, null, done)
        done.invokeOnCompletion { err ->
            post {
                if (err == null) {
                    lane.notify(ReaderNotice(if (done.unsaved()) SyncNotice.NotSaved else SyncNotice.Done(SyncAction.MARK_SERIES_COMPLETED)))
                } else {
                    lane.notify(ReaderNotice(SyncNotice.Failed(SyncAction.MARK_SERIES_COMPLETED, err)) { post { completeSeries(lane, seriesId, includeUnread) } })
                }
            }
        }
    }

    /** Restores what reading overrode, and stops tracking the item until Track progress. */
    private suspend fun undo(receipt: Int) {
        val r = receipts[receipt] ?: return
        if (frozen || (listOfNotNull(current) + queue).any { it is Write && it.receipt == receipt }) return
        val lane = r.lane
        if (!storageOk()) {
            lane.notify(ReaderNotice(SyncNotice.Failed(SyncAction.UNDO, StorageFullException())) { post { undo(receipt) } })
            return
        }
        lane.set { copy(tracking = false) }
        // The snapshot goes with the op: it survives a parked pump and a restart, which the receipt doesn't.
        val payload = JsonObject(mapOf("snapshot" to r.previous.json(), "series" to (r.series?.json() ?: JsonNull)))
        val task = Write(lane, newOp(lane, OpKind.UNDO, payload).copy(epoch = lane.row.foreignEpoch)).also { it.receipt = receipt }
        enqueue(task, listOf(lane.id))
    }

    private suspend fun clear(lane: LaneState) {
        val done = command(lane, CLEAR)
        done.invokeOnCompletion { err ->
            post {
                if (err == null && done.unsaved()) {
                    // Still queued, so it is neither done nor failed: no placement, no Retry.
                    lane.notify(ReaderNotice(SyncNotice.NotSaved))
                } else if (err == null) {
                    place(lane, EmptyProgress)
                    lane.notify(ReaderNotice(SyncNotice.Done(SyncAction.CLEAR)))
                } else {
                    lane.notify(ReaderNotice(SyncNotice.Failed(SyncAction.CLEAR, err)) { post { clear(lane) } })
                }
            }
        }
    }

    /**
     * A reviewer's Clear: nobody is there for its outcome, so it is queued without a caller and a
     * failure leaves a notice, or waits for the next drain. A reader the lane goes back to opens at the start.
     */
    private suspend fun clearUnwatched(lane: LaneState) {
        // Storage full: nothing happens, and the lane stays in Needs attention.
        if (queueCommand(lane, CLEAR, null)) place(lane, EmptyProgress)
    }

    /** Goes next, holding the item's unsent reading until it is answered. Repeats collapse. */
    private fun check(lane: LaneState) {
        fun reading(task: Task?) = task is Read && task.lane === lane
        if (frozen || !lane.row.acked || reading(current) || queue.any(::reading)) return
        lane.held = true
        queue.addFirst(Read(lane))
        pump()
    }

    // The dialog slot.

    /** Holds the one dialog slot until [onAnswer] has run without asking again; no request starts meanwhile. */
    private fun openDialog(session: Session, prompt: SyncPrompt, review: LaneState? = null, onAnswer: suspend (PromptChoice?) -> Unit) {
        if (dialog != null) return
        dialog = Dialog(session, review, onAnswer)
        effect(durable = false) { session.prompt.value = prompt }
    }

    /** A follow-up question in the open dialog. */
    private fun ask(prompt: SyncPrompt, onAnswer: suspend (PromptChoice?) -> Unit) {
        val open = checkNotNull(dialog)
        open.asked++
        open.review = null
        open.onAnswer = onAnswer
        effect(durable = false) { open.session.prompt.value = prompt }
    }

    /** A null [choice] is a dismissal: "ask again later" (P2 decision 33). */
    private suspend fun promptAnswered(session: Session, choice: PromptChoice?) {
        val open = dialog?.takeIf { it.session === session } ?: return
        val asked = open.asked
        effect(durable = false) { session.prompt.value = null }
        try {
            open.onAnswer(choice)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            onError(e)
        } finally {
            // Also when the answer failed: the one slot holds the pump and every drain.
            if (open.asked == asked) {
                dialog = null
                open.closed.complete(Unit)
            }
        }
        if (open.asked != asked) return
        // The slot is free: a review that asked ends.
        if (open.session.reviewer) open.session.leave()
        for (lane in lanes.values.toList()) if (lane.stale && lane.adapter != null && !lane.dismissed) check(lane)
        pump()
    }

    /**
     * Until its reader has loaded, it opens there instead. The reader is placed only if it hasn't
     * moved or been placed since: a restore held for a commit must not take it back.
     */
    private fun place(lane: LaneState, progress: JsonObject) {
        lane.set { copy(here = progress) }
        val session = lane.session ?: return
        if (!lane.ready) return
        val placement = session.placements.get()
        effect(durable = false) {
            scope.launch(main) {
                if (lane.adapter === session.adapter && session.placements.get() == placement) session.adapter.restore(progress)
            }
        }
    }

    private suspend fun compare(lane: LaneState, remote: Envelope) {
        val adapter = checkNotNull(lane.adapter)
        val acked = lane.row.state
        val next = remote.state
        val here = lane.row.here ?: acked.progress
        if (isCleared(next) && !isCleared(acked)) return resolve(lane, ConflictKind.RESET, remote, here)
        if (next.status == ReadingStatus.COMPLETED && acked.status != ReadingStatus.COMPLETED) {
            return resolve(lane, ConflictKind.COMPLETED, remote, here)
        }
        if (adapter.samePosition(next.progress, acked.progress)) {
            adopt(lane, remote)
            if (next.status != acked.status) lane.notify(ReaderNotice(SyncNotice.StatusElsewhere(next.status)))
            return
        }
        if (hasReading(lane)) return resolve(lane, ConflictKind.MOVED, remote, here)
        adopt(lane, remote)
        place(lane, next.progress)
        lane.notify(ReaderNotice(SyncNotice.Followed(adapter.describe(next.progress))) { post { if (lane.adapter === adapter) place(lane, here) } })
    }

    private fun resolve(lane: LaneState, kind: ConflictKind, remote: Envelope, here: JsonObject) {
        val adapter = checkNotNull(lane.adapter)
        val session = checkNotNull(lane.session)
        lane.held = true
        openDialog(session, SyncPrompt.Conflict(kind, adapter.describe(here), adapter.describe(remote.state.progress)), review = lane) { choice ->
            if (choice == null) {
                // Ask again later: nothing adopted, the lane held.
                lane.stale = true
                lane.dismissed = true
                // A check queued before the dialog would ask again at once.
                queue.removeAll { it is Read && it.lane === lane && it.done == null && !it.rebase && it.reviewer == null }
                return@openDialog
            }
            // Stay, Keep reading and Continue here send what's retained on top of it.
            val base = lane.row
            try {
                adopt(lane, remote)
                lane.held = false
                when (choice) {
                    PromptChoice.GO, PromptChoice.START -> {
                        dropReading(lane)
                        place(lane, if (choice == PromptChoice.GO) remote.state.progress else EmptyProgress)
                    }
                    PromptChoice.RESET -> ask(SyncPrompt.ConfirmClear) { if (it == PromptChoice.CONFIRM) if (session.reviewer) clearUnwatched(lane) else clear(lane) }
                    else -> Unit
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                // The choice didn't apply: the old base is back and the conflict is stored, to be asked about again.
                lane.set { copy(acked = base.acked, state = base.state, series = base.series, foreignEpoch = base.foreignEpoch) }
                lane.stale = true
                lane.held = true
                lane.dismissed = true
                throw e
            }
        }
    }

    private fun LaneState.notify(notice: ReaderNotice) {
        val session = session ?: return
        // What reports the server or a failure announces nothing stored; a Done and an Undo offer do.
        val stored = when (notice.notice) {
            is SyncNotice.Followed, is SyncNotice.StatusElsewhere, is SyncNotice.Failed, is SyncNotice.NotSaved -> false
            else -> true
        }
        effect(stored) { session.noticeChannel.trySend(notice) }
    }

    // What ReadingSyncManager exposes as ReadingSync.

    fun attach(contentId: String, adapter: ReaderAdapter): ReadingSession {
        val session = Session(contentId, adapter)
        post {
            laneOf(contentId)
            sessions += session
        }
        return session
    }

    suspend fun pageCount(contentId: String, count: Int) = actor {
        existingLane(contentId)?.set { copy(pageCount = count) }
        Unit
    }

    fun shown(ids: Set<String>): Flow<Map<String, Shown>> = store.shown(ids)

    /** Deletes a stored notice from this engine's own store, serialized with its commits; fails once the engine has stopped. */
    suspend fun dismissNotice(id: Long) = actor { store.dismissNotice(id) }

    val unsent: Flow<Int> get() = store.unsent()

    /**
     * "Needs attention" (P2 §5, Held lanes): a reviewer with the lane's own page count reads the
     * server's state and compares it as a reader would, so the same prompt asks. `restore` does
     * nothing, and nothing is offered. A reader that had the lane gets it back when the review ends ([ReadingSession.detach]).
     */
    fun review(contentId: String): ReviewSession {
        val session = Session(contentId, ReviewAdapter { lanes[contentId]?.row?.pageCount }, reviewer = true)
        // A start that failed is tried again ahead of it, as a reader's load does. If the engine can't take it, the review fails.
        retryStart()
        if (inbox.trySend(Event({ session.admit() }, reject = { session.ending() })).isFailure) session.ending()
        return session
    }

    /**
     * The app went to the background: an open prompt is dismissed, as "ask again later" (P2 decision
     * 8), and a review still reading is no longer on screen, so it won't ask.
     */
    fun background() = post {
        dialog?.let { promptAnswered(it.session, null) }
        for (session in sessions) if (session.reviewer) session.onScreen = false
    }

    suspend fun seed(contents: List<Content>) = actor {
        if (frozen) throw stopped()
        for (content in contents) seed(content)
    }

    /**
     * Imports [content]'s reading state, and makes the lane, or brings its acknowledged state up to it
     * when its seq is greater, unless the lane retains something or has a reader. `here` follows the base.
     */
    private suspend fun seed(content: Content) = seed(content.id, canonical(content.id, content.userData.readingState())) { row ->
        row.copy(
            libraryId = content.libraryId ?: row.libraryId, uri = content.uri ?: row.uri, parentId = content.parentId,
            type = content.type, title = content.title, pageCount = content.pageCount() ?: row.pageCount,
        )
    }

    /**
     * The server's newest state of [id] as the store has it, else [stated] (a response's own). A page's content
     * may carry an overlay (unsent ops, a newer snapshot's reading over an older row): it is a display, never a seed.
     */
    private suspend fun canonical(id: String, stated: ReadingState): ReadingState = store.snapshot(id)?.takeIf { it.seq >= stated.seq } ?: stated

    /** A series' listed volumes: every one gets a lane, so a series op can be shown on it. */
    private suspend fun seedVolumes(seriesId: String, volumes: SeriesVolumes) {
        for (volume in volumes.volumes) seed(volume.id, volume.userData.readingState()) { it.copy(parentId = seriesId) }
    }

    private suspend fun seed(id: String, state: ReadingState, identify: (Lane) -> Lane) {
        learn(id, state)
        val existing = existingLane(id)
        val lane = existing ?: LaneState(Lane(id)).also {
            lanes[id] = it
            noLane -= id
        }
        val row = lane.row
        val identity = identify(row)
        if (identity != row || existing == null) lane.set { identity }
        // Only a state the server stated later than the lane's base replaces it: one fetched earlier, or by a caller long ago, can't take it back.
        if (existing != null && (retains(lane) || lane.adapter != null || (row.acked && state.seq <= row.state.seq))) return
        val foreign = row.acked && existing != null && state.revision != row.state.revision && !isOurs(writerOf(state.revision))
        // As fresh as a rebase read: nothing is left to read first, nor held for it.
        if (row.rebase) lane.held = false
        lane.set { copy(acked = true, rebase = false, state = state, here = state.progress, foreignEpoch = if (foreign) foreignEpoch + 1 else foreignEpoch) }
    }

    /**
     * A content page's `set_status` or `clear` on [content] (P2 §6): its lane is seeded from
     * [content] by the ordinary rule, and then takes the command. A clear on
     * a series clears its volumes too, and is guarded as a series command is.
     */
    suspend fun command(content: Content, request: JsonObject, origin: Long? = null): CommandOutcome = submit { done ->
        if (frozen) throw stopped()
        seed(content)
        val lane = laneOf(content.id)
        if (request.string("op") == "clear" && content.isSeries) {
            withVolumes(lane, content.id, done) { volumes ->
                val covered = volumes.volumes.map { it.id }
                endReviews(covered)
                command(lane, request, done, guardOf(volumes, covered), covered, origin)
            }
        } else {
            command(lane, request, done, origin = origin)
        }
    }

    /** A content page's `mark_through` or `mark_series_completed` ([SeriesAction]) on [series], seeded as [command] seeds. */
    suspend fun seriesCommand(
        series: Content,
        action: String,
        includeUnread: Boolean = false,
        untilId: String? = null,
        origin: Long? = null,
    ): CommandOutcome =
        submit { done ->
            if (frozen) throw stopped()
            seed(series)
            seriesOp(laneOf(series.id), series.id, action, includeUnread, untilId, done, origin)
        }

    /**
     * A command's result is its op's caller from the hand-off on, and is cancelled the moment the
     * caller is, not when it next runs: a caller that leaves leaves its op, which then goes, or waits,
     * as one read back from the store does. [start] runs as an event and makes the op.
     */
    @OptIn(ExperimentalCoroutinesApi::class)
    private suspend fun submit(start: suspend (CompletableDeferred<CommandOutcome>) -> Unit): CommandOutcome {
        val done = result()
        val event = Event(
            {
                try {
                    // Before the seed and anything else: a full disk fails the command as it is made.
                    if (!storageOk()) throw StorageFullException()
                    start(done)
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    answer(done, Result.failure(e))
                }
            },
            reject = { done.completeExceptionally(it) },
        )
        if (inbox.trySend(event).isFailure) done.completeExceptionally(stopped())
        return suspendCancellableCoroutine { cont ->
            cont.invokeOnCancellation { done.cancel() }
            done.invokeOnCompletion { err -> if (err == null) cont.resume(done.getCompleted()) else cont.resumeWithException(err) }
        }
    }

    /** Lanes awaiting review; [ReadingSyncManager] adds the stored notices. */
    val held: Flow<List<AttentionItem.Held>> get() = store.held()

    /**
     * Sends everything unwatched again (P2 §5): at the start, on the foreground, from the worker and
     * "Sync now". Answers once the pump comes to rest: `DONE` with nothing left but held lanes, else `WAITING`; `BUSY` while a prompt is open.
     */
    suspend fun drain(): DrainResult {
        val result = actor {
            if (frozen) throw stopped()
            if (dialog != null) return@actor CompletableDeferred(DrainResult.BUSY)
            drainPass()
            CompletableDeferred<DrainResult>().also {
                pending += it
                it.invokeOnCompletion { _ -> pending -= it }
                drains += it
            }
        }
        return result.await()
    }

    /** The web's `attach()` object. */
    private inner class Session(val contentId: String, val adapter: ReaderAdapter, val reviewer: Boolean = false) : ReviewSession {
        override val view = MutableStateFlow(LaneView())
        override val prompt = MutableStateFlow<SyncPrompt?>(null)
        override val outcome = MutableStateFlow<ReviewOutcome?>(null)
        override val ended = MutableStateFlow(false)
        private var left = false
        val noticeChannel = Channel<ReaderNotice>(Channel.UNLIMITED)
        override val notices: Flow<ReaderNotice> = noticeChannel.receiveAsFlow()
        var onScreen = true
        private var shown = false

        /** Counts the reader's own moves and placements, as they are made. */
        val placements = AtomicInteger()

        private fun mine(lane: LaneState) = lane.adapter === adapter && !frozen

        /** The reader a review took the lane from, to give it back, and where it stood then. */
        private var before: Triple<ReaderAdapter, Session, Boolean>? = null
        private var hereBefore: JsonObject? = null

        /** [review], where any failure ends the session as failed and gives the lane back as it was. */
        suspend fun admit() {
            try {
                review()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                onError(e)
                sessions -= this
                lanes[contentId]?.takeIf { it.session === this }?.let { lane ->
                    lane.adapter = before?.first
                    lane.session = before?.second
                    lane.ready = before?.third ?: false
                }
                ending()
            }
        }

        /** Takes the lane and reads it; a difference asks, the rest resolves at once. */
        suspend fun review() {
            val lane = if (frozen) null else existingLane(contentId)?.takeIf { it.row.acked }
            if (lane == null) return reviewed(ReviewOutcome.RESOLVED)
            sessions += this
            lane.session?.takeIf { it in sessions }?.let { before = Triple(it.adapter, it, lane.ready) }
            hereBefore = lane.row.here
            lane.adapter = adapter
            lane.session = this
            lane.ready = false
            lane.dismissed = false
            check(lane)
            // A check already queued reads for it. Parked, it wouldn't go; behind a load or rebase read, there is none.
            val read = (listOfNotNull(current) + queue).firstOrNull { it is Read && it.lane === lane && it.done == null && !it.rebase } as Read?
            if (read == null || serverParked) reviewed(ReviewOutcome.FAILED) else read.reviewer = this
        }

        /** The engine can't go on with it (stopped, or never started): a review that isn't answered failed, and ended. */
        fun ending() {
            if (!reviewer) return
            left = true
            if (outcome.value == null) outcome.value = ReviewOutcome.FAILED
            ended.value = true
        }

        /** Its read was answered: [ReviewOutcome.ASKED] lasts until the dialog slot is free, anything else ends it now. */
        suspend fun reviewed(result: ReviewOutcome) {
            if (!reviewer || left) return
            effect(durable = false) { outcome.value = result }
            if (result != ReviewOutcome.ASKED) leave()
        }

        private fun onLane(block: suspend (LaneState) -> Unit) = post { block(laneOf(contentId)) }

        override suspend fun load(): JsonObject = actor {
            if (frozen) throw stopped()
            val lane = laneOf(contentId)
            sessions += this
            lane.adapter = adapter
            lane.session = this
            lane.ready = false
            lane.dismissed = false
            adapter.content()?.let { content ->
                lane.set {
                    copy(
                        libraryId = content.libraryId ?: libraryId, uri = content.uri ?: uri, parentId = content.parentId,
                        type = content.type, title = content.title, pageCount = content.pageCount() ?: pageCount,
                    )
                }
            }
            // Its reader is here now: what waited for a drain is tried again.
            for (task in queue) if (task is Write && task.lane === lane) task.drainWait = false
            val done = CompletableDeferred<JsonObject>()
            // Without the server, an acknowledged lane opens at once, from what is stored (P2 §5, Opening).
            if (lane.row.acked && (serverParked || !connectivity.online.value)) fallback(lane, done) else push(Read(lane, done, quick = lane.row.acked))
            done
        }.await()

        override suspend fun pageCount(count: Int) = actor {
            if (frozen) throw stopped()
            laneOf(contentId).set { copy(pageCount = count) }
            Unit
        }

        override fun invalidateRestores() {
            placements.incrementAndGet()
        }

        override fun moved(progress: JsonObject) {
            placements.incrementAndGet()
            movedOnLane(progress)
        }

        private fun movedOnLane(progress: JsonObject) = onLane { lane ->
            if (!mine(lane) || !lane.row.tracking) return@onLane
            val finished = finishedAt(lane)
            if (finished != null && adapter.samePlace(progress, finished)) return@onLane
            // While its sends fail or the pump is parked, a run of reading is one write (P2 decision 25).
            val own = queue.lastOrNull { it is Write && it.lane === lane } as Write?
            // Also while the lane is held (a dismissed conflict): nothing of it goes before the answer, so only the latest matters.
            val merge = own?.takeIf { it.op.kind == OpKind.POSITION && it.op.sentSeq == null && (lane.row.failed || lane.held || parked.isNotEmpty()) }
            lane.rebaseFailed = false
            lane.set { copy(here = progress, failed = false) }
            val last = queue.lastOrNull()
            if (last is Write && last.op.kind == OpKind.POSITION && last.lane === lane && !last.op.sealed) {
                last.set { copy(payload = progress) }
            } else if (merge != null) {
                merge.set { copy(payload = progress) }
            } else {
                Write(lane, newOp(lane, OpKind.POSITION, progress)).let {
                    hold()
                    added += it
                    push(it)
                }
            }
            timer?.cancel()
            lateinit var debounce: Job
            debounce = scope.launch {
                delay(WRITE_DEBOUNCE)
                post { if (timer === debounce) sendNow() }
            }
            timer = debounce
        }

        override fun placed(progress: JsonObject) {
            placements.incrementAndGet()
            placedOnLane(progress)
        }

        private fun placedOnLane(progress: JsonObject) = onLane { lane ->
            if (!mine(lane)) return@onLane
            lane.set { copy(here = progress) }
            sendNow()
        }

        override fun finish(progress: JsonObject) = onLane { lane ->
            if (!mine(lane) || !lane.row.tracking || finishedAt(lane) != null) return@onLane
            lane.rebaseFailed = false
            lane.set { copy(here = progress, failed = false) }
            Write(lane, newOp(lane, OpKind.FINISH, progress)).let {
                hold()
                added += it
                push(it)
            }
        }

        override fun flush() = post { sendNow() }

        override suspend fun command(request: JsonObject): CommandOutcome = submit { done -> command(laneOf(contentId), request, done) }

        override suspend fun seriesCommand(status: String): CommandOutcome = submit { done -> command(laneOf(contentId), seriesStatus(status), done) }

        override fun resetAndReadAgain() = onLane { lane ->
            openDialog(this, SyncPrompt.ConfirmClear) { if (it == PromptChoice.CONFIRM) clear(lane) }
        }

        override fun completeSeries() = onLane { lane ->
            val series = lane.row.series ?: return@onLane
            val unread = series.childrenCount - series.completedChildrenCount - series.droppedChildrenCount
            openDialog(this, SyncPrompt.ConfirmCompleteSeries(unread)) { choice ->
                // Its unread volumes, as the server lists them, are completed with it.
                if (choice == PromptChoice.CONFIRM || choice == PromptChoice.CONFIRM_WITH_UNREAD) {
                    completeSeries(lane, series.id, includeUnread = choice == PromptChoice.CONFIRM_WITH_UNREAD)
                }
            }
        }

        override fun trackProgress() = onLane { lane -> lane.set { copy(tracking = true) } }

        override fun retry() = onLane { lane ->
            lane.rebaseFailed = false
            lane.set { copy(failed = false) }
            for (task in queue) if (task is Write && task.lane === lane) task.drainWait = false
            seal()
            reconnected()
        }

        override fun check() = onLane { lane ->
            if (!mine(lane)) return@onLane
            lane.dismissed = false
            check(lane)
        }

        override fun setVisible(visible: Boolean) = onLane { lane ->
            onScreen = visible
            if (!visible) {
                shown = true
                seal()
                pump()
            } else if (mine(lane) && !lane.dismissed) {
                // The first time on screen, a read that load() just answered is as fresh as a check.
                val fresh = !shown && lane.ready && !lane.unchecked
                shown = true
                if (!fresh) check(lane)
            }
        }

        override fun detach() = post { leave() }

        /** Once. A reviewer gives the lane back to the reader it took it from, placed where the review left it. */
        suspend fun leave() {
            if (left) return
            left = true
            onScreen = false
            sessions -= this
            // Its dialog goes with it, also when another reader has the item now.
            dialog?.takeIf { it.session === this }?.let { promptAnswered(this, null) }
            effect(durable = false) { noticeChannel.close() }
            if (reviewer) effect(durable = false) { ended.value = true }
            val lane = lanes[contentId] ?: return
            if (lane.adapter !== adapter) return
            lane.adapter = null
            lane.session = null
            before?.takeIf { (_, reader) -> reader in sessions }?.let { (adapter, reader, ready) ->
                lane.adapter = adapter
                lane.session = reader
                lane.ready = ready
                // Go there, or a Clear, moved it during the review.
                lane.row.here?.takeIf { it != hereBefore }?.let { place(lane, it) }
            }
            sendNow()
            if (lane.dirty) {
                lane.dirty = false
                val change = CatalogChange.ReadingChanged(lane.id, lane.parentOf)
                effect { events.emit(change) }
            }
        }

        override fun answer(choice: PromptChoice) = post { promptAnswered(this, choice) }

        override fun dismissPrompt() = post { promptAnswered(this, null) }
    }

    companion object {
        const val WRITE_DEBOUNCE = 1000L

        /** How soon a commit the store refused is tried again when no event comes. */
        const val STORAGE_RETRY = 5000L

        /** Lanes untouched this long, with nothing unsent and nothing to ask, are pruned at the start. */
        const val PRUNE_AFTER = 30L * 24 * 60 * 60 * 1000

        /** The most ids the server takes in a series write (`ids`). */
        const val MAX_IDS = 10_000

        /** The wait before the probe after the second unreachable answer in a row; it doubles up to [PROBE_BACKOFF_MAX]. */
        const val PROBE_BACKOFF = 5000L
        const val PROBE_BACKOFF_MAX = 60_000L

        private fun stopped() = IllegalStateException("The reading sync stopped")

        private val CLEAR = JsonObject(mapOf("op" to JsonPrimitive("clear")))

        /** The reader that wrote [revision], if one did: theirs are `<writer_id>:<seq>`. */
        private fun writerOf(revision: String?) = revision?.takeIf { it.startsWith("t") }?.substringBefore(':')

        /** Cleared, as a clear leaves it: a status cleared on its own keeps its time. */
        private fun isCleared(s: ReadingState) =
            s.status == null && s.progress.isEmpty() && s.statusUpdatedAt.isNullOrEmpty() && s.lastReadAt.isNullOrEmpty()

        private fun JsonObject.string(key: String) = (this[key] as? JsonPrimitive)?.contentOrNull

        // Written out, nulls included: the server reads these as they are.
        private fun text(value: String?) = JsonPrimitive(value)

        private fun Snapshot.json() = JsonObject(mapOf("status" to text(status), "progress" to progress, "last_read_at" to text(lastReadAt)))

        private fun SeriesPrevious.json() =
            JsonObject(mapOf("revision" to text(revision), "status" to text(status), "status_updated_at" to text(statusUpdatedAt)))
    }
}

/**
 * A reviewer's adapter: the lane's page count when it is known. Without one, positions compare as if
 * the item were endless, and are described by their saved percentage, or as the end. `restore` does nothing.
 */
private class ReviewAdapter(private val pages: () -> Int?) : ReaderAdapter {
    private val comic = ComicAdapter({ pages() ?: Int.MAX_VALUE })

    override fun content() = null

    override fun restore(progress: JsonObject) = Unit

    override fun describe(progress: JsonObject): PositionLabel {
        if (pages() != null) return comic.describe(progress)
        if (progress["at_end"] == JsonPrimitive(true)) return PositionLabel.End
        val percent = (progress["progress_percent"] as? JsonPrimitive)?.doubleOrNull
        return percent?.let { PositionLabel.Percent(it.roundToInt()) } ?: PositionLabel.Page(pageFor(progress, Int.MAX_VALUE) + 1)
    }

    override fun samePosition(a: JsonObject, b: JsonObject) = comic.samePosition(a, b)

    override fun samePlace(a: JsonObject, b: JsonObject) = comic.samePlace(a, b)
}
