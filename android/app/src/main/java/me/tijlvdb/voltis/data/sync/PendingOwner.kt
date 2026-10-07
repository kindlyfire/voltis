package me.tijlvdb.voltis.data.sync

import android.util.Log
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.AccountChangedException
import me.tijlvdb.voltis.data.api.UnexpectedResponse
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.reading.SyncScheduler
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.net.settled
import me.tijlvdb.voltis.domain.reading.DrainResult
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.domain.sync.NoticeDetail
import me.tijlvdb.voltis.domain.sync.NoticeKind
import me.tijlvdb.voltis.domain.sync.UNREADABLE_ERROR
import me.tijlvdb.voltis.domain.sync.PendingChange
import me.tijlvdb.voltis.domain.sync.PendingStore
import me.tijlvdb.voltis.domain.sync.PendingUserData
import me.tijlvdb.voltis.domain.sync.StoredNotice
import me.tijlvdb.voltis.domain.sync.UserDataItem

/**
 * Who asked for a run: a foreground run needs the server to be reachable, a background one doesn't look.
 * [STARTUP] is the owner's first run and its reconnections while the app isn't in the foreground: they look like a
 * foreground run, but can happen in a process only a worker started, so alone (or with the one-time worker) they leave stuck rows be.
 */
internal enum class RunKind { FOREGROUND, ONE_TIME, PERIODIC, STARTUP }

/**
 * Everything of one account store's pending rows (P4 §8): the rows' writes, the run loop and its scope.
 * Made only for the store `AccountStores.current` holds, by `PendingSync`, and stopped before that store closes.
 * Every write runs in [scope], so a [stop] cancels it or waits for it and never closes the database under it.
 */
internal class PendingOwner(
    val account: String,
    val store: PendingStore,
    private val transport: PendingTransport,
    private val connectivity: Connectivity,
    private val scheduler: SyncScheduler,
    /** After the commit that stored them (`RoomSyncNotices.announce`). */
    private val announce: suspend (List<StoredNotice>) -> Unit,
    private val events: CatalogEvents,
    dispatcher: CoroutineDispatcher = Dispatchers.Default,
    private val clock: () -> Long = System::currentTimeMillis,
    private val onError: (String, Throwable) -> Unit = { message, e -> Log.e(TAG, message, e) },
    /** Live: whether the app is in the foreground (`PendingSync`'s flag), so a start or a reconnection then asks as the foreground. */
    private val foreground: () -> Boolean = { false },
) {
    private val job = SupervisorJob()
    private val scope = CoroutineScope(dispatcher + job)

    private fun startupKind() = if (foreground()) RunKind.FOREGROUND else RunKind.STARTUP

    /** Held by a run for its whole pass, and by Retry and Discard. */
    val mutex = Mutex()

    private class Batch {
        val kinds = mutableSetOf<RunKind>()
        val result = CompletableDeferred<DrainResult>()
    }

    private val lock = Any()
    private var closed = false // guarded by lock
    private var next: Batch? = null // guarded by lock
    private val wake = Channel<Unit>(Channel.CONFLATED)

    /** Rows with a wish. */
    val unsent: Flow<Int> = store.watchWanted().map { it.size }.distinctUntilChanged()

    /** Rows with a wish that failed in three runs. */
    val stuck: Flow<List<PendingUserData>> = store.watchWanted().map { rows -> rows.filter { it.stuck } }

    /**
     * Before the owner is published: the prune, best effort (a full disk must not keep rows that could
     * be sent for nothing from being stored and sent: stale landed columns are older than any load's stamp,
     * so they show nothing), the loop, the `online` collector, and a first run for what an earlier sign-in or a kill left.
     */
    suspend fun start() {
        try {
            store.prune()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            onError("Couldn't prune the pending rows", e)
        }
        scope.launch { loop() }
        scope.launch { connectivity.online.drop(1).collect { if (it) request(startupKind()) } }
        request(startupKind())
    }

    /** Stored, then asks for a run; nothing waits on the server. */
    suspend fun submit(item: UserDataItem, starred: Boolean?, rating: JsonElement?) {
        inScope { store.commit(PendingChange.Submit(item, starred, rating, clock())) }
        request(RunKind.FOREGROUND)
    }

    suspend fun retry(contentId: String) {
        inScope { mutex.withLock { store.commit(PendingChange.Reset(contentId)) } }
        request(RunKind.FOREGROUND)
    }

    /** Clears the wish and emits nothing: the server never had it. */
    suspend fun discard(contentId: String) {
        inScope { mutex.withLock { store.commit(PendingChange.Discard(contentId)) } }
    }

    /**
     * One more run after the one going, at most: every caller of a batch gets the result of the one run
     * that starts after it asked. Cancelling a waiter doesn't cancel the run. Fails with
     * [SyncUnavailable.AccountChanged] once the owner stopped.
     */
    fun request(kind: RunKind): Deferred<DrainResult> = synchronized(lock) {
        if (closed) return CompletableDeferred<DrainResult>().also { it.completeExceptionally(SyncUnavailable.AccountChanged()) }
        val batch = next ?: Batch().also { next = it }
        batch.kinds += kind
        wake.trySend(Unit)
        batch.result
    }

    /** Admission closes, then the scope (runs, writes, the collector) is cancelled and joined. */
    suspend fun stop() {
        synchronized(lock) {
            closed = true
            next?.result?.completeExceptionally(SyncUnavailable.AccountChanged())
            next = null
        }
        wake.close()
        job.cancelAndJoin()
    }

    private suspend fun <T> inScope(block: suspend () -> T): T {
        if (synchronized(lock) { closed }) throw SyncUnavailable.AccountChanged()
        try {
            return scope.async { block() }.await()
        } catch (e: CancellationException) {
            // The caller's own cancellation stays one; the owner's is an account change.
            currentCoroutineContext().ensureActive()
            throw SyncUnavailable.AccountChanged()
        }
    }

    private suspend fun loop() {
        for (ignored in wake) {
            while (true) {
                val batch = synchronized(lock) { next.also { next = null } } ?: break
                val result = try {
                    mutex.withLock { pass(batch.kinds) }
                } catch (e: CancellationException) {
                    batch.result.completeExceptionally(SyncUnavailable.AccountChanged())
                    throw e
                } catch (e: Exception) {
                    onError("A pending run failed", e)
                    scheduler.schedule()
                    DrainResult.WAITING
                }
                batch.result.complete(result)
            }
        }
    }

    private enum class Step { NEXT, END, FULL }

    private suspend fun pass(kinds: Set<RunKind>): DrainResult {
        val background = kinds.any { it == RunKind.ONE_TIME || it == RunKind.PERIODIC }
        // Only the one-time worker and the owner's own startup leave stuck rows alone; a run that also serves anything else tries them.
        val skipStuck = kinds.all { it == RunKind.ONE_TIME || it == RunKind.STARTUP }
        // Targets that failed in this run: a repeat would only fail the same way.
        val aside = mutableSetOf<String>()
        if (!background) {
            connectivity.settled()
            if (!connectivity.online.value) return end()
        }
        while (true) {
            val row = store.wanted().firstOrNull { it.contentId !in aside && !(skipStuck && it.stuck) } ?: break
            when (send(row, aside)) {
                Step.NEXT -> Unit
                Step.END -> return end()
                Step.FULL -> {
                    scheduler.schedule()
                    return DrainResult.WAITING
                }
            }
        }
        return end()
    }

    /** A row that can still be tried later asks for the background worker. */
    private suspend fun end(): DrainResult {
        val left = store.wanted().any { !it.stuck }
        if (!left) return DrainResult.DONE
        scheduler.schedule()
        return DrainResult.WAITING
    }

    private suspend fun send(row: PendingUserData, aside: MutableSet<String>): Step {
        val fields = buildMap {
            row.starred?.let { put("starred", JsonPrimitive(it)) }
            if (row.ratingSet) put("rating", row.rating?.let { JsonPrimitive(it) } ?: JsonNull)
        }
        val answer = try {
            transport.updateUserData(row.contentId, JsonObject(fields))
        } catch (e: CancellationException) {
            throw e
        } catch (e: AccountChangedException) {
            // Not an answer: the owner is being stopped. Nothing counted, no probe.
            return Step.END
        } catch (e: ReadingFailure) {
            return when (e) {
                is ReadingFailure.Refused -> drop(row, NoticeKind.REFUSED, NoticeDetail(message = e.message))
                is ReadingFailure.Gone -> drop(row, NoticeKind.GONE, NoticeDetail())
                is ReadingFailure.SignedOut -> Step.END
                // The result of a 5xx is unknown, but a repeat is harmless.
                is ReadingFailure.ServerError -> fail(row, e.message ?: "Server error ${e.code}", aside)
                is ReadingFailure.Unreachable ->
                    // No probe, no server: the run ends with nothing counted. A server that answers the probe has a problem with this request.
                    if (!connectivity.probe()) Step.END else fail(row, if (e.cause is UnexpectedResponse) UNREADABLE_ERROR else e.message ?: "Can't reach the server", aside)
                else -> fail(row, e.message ?: "Failed", aside)
            }
        }
        if (commit(PendingChange.Landed(row.contentId, row.rev, answer)) == null) return Step.FULL
        events.emit(CatalogChange.UserDataChanged(row.contentId))
        return Step.NEXT
    }

    private suspend fun drop(row: PendingUserData, kind: String, detail: NoticeDetail): Step {
        val notice = StoredNotice(0, row.contentId, row.title, kind, detail, clock())
        val stored = commit(PendingChange.Dropped(row.contentId, row.rev, notice)) ?: return Step.FULL
        if (stored.isNotEmpty()) announce(stored)
        return Step.NEXT
    }

    private suspend fun fail(row: PendingUserData, error: String, aside: MutableSet<String>): Step {
        aside += row.contentId
        commit(PendingChange.Failed(row.contentId, row.rev, error)) ?: return Step.FULL
        return Step.NEXT
    }

    /** Null when the disk is full: the rows are as they were. */
    private suspend fun commit(change: PendingChange): List<StoredNotice>? = try {
        store.commit(change)
    } catch (e: StorageFullException) {
        null
    }

    private companion object {
        const val TAG = "PendingOwner"
    }
}
