package me.tijlvdb.voltis.data.reading

import android.content.Context
import dagger.hilt.android.qualifiers.ApplicationContext
import java.util.concurrent.atomic.AtomicLong
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.data.auth.AppScope
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.db.AccountScoped
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.data.db.OpenAccount
import me.tijlvdb.voltis.domain.catalog.BulkCommand
import me.tijlvdb.voltis.domain.catalog.BulkWhat
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.reading.ReadingCommands
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.SyncCommand
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.reading.titled
import me.tijlvdb.voltis.domain.sync.NoticeDetail
import me.tijlvdb.voltis.domain.sync.NoticeKind
import me.tijlvdb.voltis.domain.sync.StoredNotice
import me.tijlvdb.voltis.domain.sync.SyncNotices

/**
 * A running batch: [done] and [failed] of [total] so far. [notSaved]: commands the engine kept in memory
 * because storage is full (neither done nor failed: they are sent once space is back, and never resubmitted).
 * [unsaved]: failure notices counted but not yet stored; with all commands run, the batch is Saving.
 */
data class BulkState(val id: Long, val what: BulkWhat, val total: Int, val done: Int = 0, val failed: Int = 0, val unsaved: Int = 0, val notSaved: Int = 0) {
    val finished get() = done + failed + notSaved
    val saving get() = finished == total && unsaved > 0
}

/** A batch's end; every one of its [failed] is a stored notice. [notSaved]: commands kept in memory on a full disk. [store]: the store it ran under, by identity. */
data class BulkSummary(val id: Long, val store: OpenAccount, val what: BulkWhat, val done: Int, val failed: Int, val notSaved: Int = 0) {
    val account get() = store.account
}

/**
 * Runs a bulk status or clear outside any screen (P4 §6), one command at a time and one batch at a
 * time. Its commands are bound to the account it started under and tagged with the batch id, so
 * [CatalogEvents] holds their changes for one refresh at the end. Every failure is stored as a notice
 * at once; the summary is published only when all are stored, and queues until presented. Account-scoped:
 * teardown cancels the batch and says nothing.
 */
@Singleton
class BulkRunner(
    private val commandsFor: (account: String, origin: Long) -> ReadingCommands,
    private val notices: SyncNotices,
    private val events: CatalogEvents,
    scope: CoroutineScope,
    private val opened: StateFlow<OpenAccount?>,
    private val describe: (Throwable) -> String,
    private val now: () -> Long = System::currentTimeMillis,
) : AccountScoped {
    @Inject constructor(
        sync: ReadingSyncManager,
        notices: SyncNotices,
        events: CatalogEvents,
        stores: AccountStores,
        @AppScope scope: CoroutineScope,
        @ApplicationContext context: Context,
    ) : this(sync::commandsFor, notices, events, scope, stores.current, { it.toUiText().string(context) }) {
        stores.register(this)
    }

    private val ids = AtomicLong()
    private val batches = CoroutineScope(scope.coroutineContext + SupervisorJob(scope.coroutineContext[Job]))

    private val gate = Any()

    private class Active(val id: Long, val job: Job)

    private var active: Active? = null // guarded by gate
    private var closing = false // guarded by gate: admission closed during teardown

    private val _state = MutableStateFlow<BulkState?>(null)

    /** Null while nothing runs or saves. */
    val state: StateFlow<BulkState?> = _state.asStateFlow()

    private val _summaries = MutableStateFlow<List<BulkSummary>>(emptyList())

    /** Batch ends still to present, oldest first. An entry goes by [acknowledge], or at teardown of its store. */
    val summaries: StateFlow<List<BulkSummary>> = _summaries.asStateFlow()

    /** The batch's id; null while another runs or saves, during teardown, or when [account]'s store isn't the open one. */
    fun start(account: String, what: BulkWhat, batch: List<BulkCommand>): Long? {
        val id: Long
        val job: Job
        synchronized(gate) { // id, job, state and cleanup handler: installed together, never seen half-done
            if (closing || active != null) return null
            val store = opened.value?.takeIf { it.account == account } ?: return null
            id = ids.incrementAndGet()
            job = batches.launch(start = CoroutineStart.LAZY) { execute(id, store, what, batch) }
            active = Active(id, job)
            _state.value = BulkState(id, what, batch.size)
            // However the job ends, even cancelled before it ran: it frees its own slot, and no other.
            job.invokeOnCompletion {
                synchronized(gate) {
                    if (active?.id == id) {
                        active = null
                        _state.value = null
                    }
                }
            }
        }
        job.start()
        return id
    }

    fun acknowledge(id: Long) = _summaries.update { q -> q.filterNot { it.id == id } }

    /** Still to present: queued, and its store is the open one. */
    fun pending(s: BulkSummary) = opened.value === s.store && _summaries.value.any { it.id == s.id }

    fun pendingFlow(s: BulkSummary): Flow<Boolean> =
        combine(opened, _summaries) { store, q -> store === s.store && q.any { it.id == s.id } }.distinctUntilChanged()

    /**
     * Account teardown only (AccountStores, after `current` went null). Cancels and joins the active
     * batch, snapshotted under the gate; no batch is admitted until its cleanup is done. Drops only
     * summaries whose store is no longer open: at teardown that is the closing store's.
     */
    override suspend fun stop() {
        val job = synchronized(gate) {
            closing = true
            active?.job
        }
        try {
            job?.cancelAndJoin() // its completion handler has freed the slot before join returns
            _summaries.update { q -> q.filter { it.store === opened.value } }
        } finally {
            synchronized(gate) { closing = false }
        }
    }

    private suspend fun execute(id: Long, store: OpenAccount, what: BulkWhat, batch: List<BulkCommand>) {
        fun cut() = opened.value !== store
        if (cut()) return
        val commands = commandsFor(store.account, id)
        var done = 0
        var failed = 0
        var notSaved = 0
        val unsaved = ArrayDeque<StoredNotice>()
        fun progress() = _state.update { if (it?.id == id) it.copy(done = done, failed = failed, unsaved = unsaved.size, notSaved = notSaved) else it }
        events.begin(id)
        try {
            for (command in batch) {
                if (cut()) return
                var outcome: CommandOutcome? = null
                val failure = try {
                    outcome = run(commands, command)
                    null
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    e
                }
                // current is nulled before the reading sync stops: "sync stopped" errors are always seen as cut.
                if (cut() || failure is SyncUnavailable.AccountChanged) return
                if (failure == null) {
                    // UNSAVED: the engine holds it in memory and sends it when space is back; not done, not failed, never run again.
                    if (outcome == CommandOutcome.UNSAVED) notSaved++ else done++
                } else {
                    failed++
                    val notice = notice(command, failure)
                    if (!insert(store, notice)) unsaved += notice
                    if (cut()) return
                }
                progress()
            }
        } finally {
            // Exactly once per begin; the refresh isn't delayed by Saving.
            events.end(id, batch.map { it.touched })
        }
        // Saving: the summary says every failure is listed, so each is stored first. Commands never run again.
        var wait = 1_000L
        while (unsaved.isNotEmpty()) {
            delay(wait)
            wait = (wait * 2).coerceAtMost(30_000L)
            if (cut()) return
            while (unsaved.isNotEmpty() && insert(store, unsaved.first())) unsaved.removeFirst()
            if (cut()) return
            progress()
        }
        if (cut()) return
        // No suspension between this check and the update; teardown joins this job before it filters the queue.
        _summaries.update { it + BulkSummary(id, store, what, done, failed, notSaved) }
    }

    /** Into the batch's own [store], and only while it is the open one. announce = false: the summary counts it. False when it couldn't be stored. */
    private suspend fun insert(store: OpenAccount, notice: StoredNotice): Boolean = if (opened.value !== store) false else try {
        notices.insert(store, notice, announce = false)
        true
    } catch (e: CancellationException) {
        throw e
    } catch (_: Exception) {
        false
    }

    private val BulkCommand.touched
        get() = item.content.let { CatalogChange.ReadingChanged(it.id, it.parentId ?: it.id) }

    private suspend fun run(commands: ReadingCommands, command: BulkCommand): CommandOutcome {
        val content = command.item.content
        // A refusal to queue at all is a StorageFullException: a failure, stored as a notice like the others.
        return when (command) {
            is BulkCommand.SetStatus -> commands.setStatus(content, command.status)
            is BulkCommand.CompleteSeries -> commands.markSeriesCompleted(content, command.includeUnread)
            is BulkCommand.Clear -> commands.clear(content)
        }
    }

    private fun notice(command: BulkCommand, failure: Exception): StoredNotice {
        val (content, series) = command.item
        val (kind, detail) = when (failure) {
            is ReadingFailure.ChangedElsewhere -> NoticeKind.CHANGED_ELSEWHERE to NoticeDetail(command = failure.command, uncertain = failure.uncertain)
            is ReadingFailure.Gone -> NoticeKind.GONE to NoticeDetail()
            else -> NoticeKind.REFUSED to NoticeDetail(command = command.name, message = describe(failure))
        }
        return StoredNotice(0, content.id, titled(series, content.title), kind, detail, now())
    }

    private val BulkCommand.name
        get() = when (this) {
            is BulkCommand.Clear -> SyncCommand.CLEAR
            is BulkCommand.CompleteSeries -> SyncCommand.MARK_SERIES_COMPLETED
            is BulkCommand.SetStatus -> if (status == ReadingStatus.COMPLETED) SyncCommand.MARK_COMPLETED else null
        }
}
