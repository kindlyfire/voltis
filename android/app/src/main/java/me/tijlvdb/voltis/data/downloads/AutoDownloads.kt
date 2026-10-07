package me.tijlvdb.voltis.data.downloads

import android.database.sqlite.SQLiteException
import android.util.Log
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.FlowPreview
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.conflate
import kotlinx.coroutines.flow.distinctUntilChangedBy
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.merge
import kotlinx.coroutines.flow.sample
import kotlinx.coroutines.flow.transformLatest
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withTimeoutOrNull
import me.tijlvdb.voltis.data.reading.ReadingHolds
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.storage.StorageFullException

/**
 * The triggers of automatic downloads (P2 §17). Each only says "reconcile this owner": the rule and its
 * facts are the owner's. A trigger lost to a kill is made up by the next one.
 */
internal class AutoDownloads(
    /** The authoritative current owner. */
    private val current: () -> DownloadStore?,
    /** The repository's owner flow, for [settle]. */
    private val owners: StateFlow<DownloadStore?>,
    private val reconcile: suspend (DownloadStore, Set<String>?) -> AutoApplied = { store, series -> store.reconcileAuto(series) },
    /** `ensureScheduled`: starts the work when the owner has runnable rows. */
    private val schedule: suspend (DownloadStore, replace: Boolean, wake: Long?) -> Unit,
    private val clock: () -> Long,
    private val retryAfter: Long = RETRY_MS,
    private val ownerWait: Long = OWNER_WAIT_MS,
    /** The churning sources (a running transfer commits a row per page) are read at most this often. */
    private val sampleMs: Long = SAMPLE_MS,
) {
    /** The scheduling retry of [owner]: kept until its [follow] picks it up. [id] tells a re-armed request from the same one. */
    private class RetryRequest(val owner: DownloadStore, val replace: Boolean, val armedAt: Long, val id: Long)

    private val retry = MutableStateFlow<RetryRequest?>(null)
    private var requests = 0L
    private val following = ConcurrentHashMap.newKeySet<Job>()

    /**
     * Asks for the scheduling to be tried again 30 s after the first request, for [store] while it is the
     * current owner. [replace] is kept (OR-ed) until it has run; a stale owner is rejected.
     */
    fun retrySoon(store: DownloadStore, replace: Boolean) = arm(store, replace, fresh = false)

    private fun arm(store: DownloadStore, replace: Boolean, fresh: Boolean) {
        if (store !== current()) return
        retry.update { old ->
            // Checked again in every attempt: an owner change between the check above and here wins over this write.
            if (store !== current()) return@update old
            val same = old?.takeIf { it.owner === store }
            if (same != null && !fresh) {
                if (same.replace || !replace) same else RetryRequest(store, true, same.armedAt, same.id)
            } else {
                RetryRequest(store, replace || same?.replace == true, clock(), ++requests)
            }
        }
    }

    /** The owner changed (the callback may run late): a request of a store that isn't the authoritative current owner is dropped. */
    fun ownerChanged() {
        retry.update { it?.takeIf { r -> r.owner === current() } }
    }

    /**
     * Follows [store] until it is replaced (cancelled), stops or fails: after every commit to what the rule reads
     * (the download and content tables sampled, as a running transfer commits per page), a reader's last hold
     * going, the engine's hold clearing, a commit that made rows runnable, or the scheduling retry, it works out
     * the plan once, without the lock, and reconciles the series whose plan isn't empty. A wake schedules the work
     * whatever the plan is: the owner's lifetime, not its caller's, carries a committed change to WorkManager.
     */
    @OptIn(ExperimentalCoroutinesApi::class, FlowPreview::class)
    suspend fun follow(store: DownloadStore) {
        val job = currentCoroutineContext()[Job]
        job?.let(following::add)
        try {
            val dir = store.accountStore.dir.name
            val due = AtomicBoolean(false)
            val timer = retry.map { it?.takeIf { r -> r.owner === store } }
                .distinctUntilChangedBy { it?.id }
                .transformLatest { request ->
                    if (request != null) {
                        delay((request.armedAt + retryAfter - clock()).coerceAtLeast(0))
                        due.set(true)
                        emit(Unit)
                    }
                }
            val tracker = store.accountStore.db.invalidationTracker
            merge(
                tracker.createFlow("series_policy", "auto_offer", "reading_lane", "reading_op", "reading_snapshot").map { },
                // Sampled, not debounced: the last change of a burst still comes through.
                tracker.createFlow("content", "download").sample(sampleMs).map { },
                store.wakes.map { },
                store.unpinned.map { },
                ReadingHolds.released(dir).map { },
                timer,
            ).conflate().catch { e ->
                // An upstream failure (the database closing under an account switch) ends this follow; the next owner has its own.
                if (e is SyncUnavailable) throw e
                Log.w(TAG, "Stopped following the automatic downloads of ${store.account}", e)
            }.collect {
                // The request this emission schedules for, if the timer fired (the flag survives conflation).
                val request = if (due.getAndSet(false)) retry.value?.takeIf { it.owner === store } else null
                try {
                    val work = store.autoWork()
                    if (work.isNotEmpty()) reconcile(store, work)
                    // Read after the reconcile, which may raise a wake of its own. Skipped when a caller already submitted it.
                    val wake = store.wakes.value
                    if (request == null && wake > store.servicedWakes) schedule(store, false, wake)
                    if (request != null) {
                        // A retry always submits: its failed submission may have had no wake.
                        schedule(store, request.replace, null)
                        if (!retry.compareAndSet(request, null)) {
                            // Upgraded to replace while it ran: that part wasn't serviced, and its timer already fired.
                            val left = retry.value?.takeIf { it.owner === store && it.id == request.id }
                            if (left != null && left.replace && !request.replace) arm(store, true, fresh = true)
                        }
                    }
                } catch (e: SyncUnavailable) {
                    throw e
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    // The next emission or the retry tries again. A refused commit, WorkManager's failure and a failed read are
                    // expected; anything else is a defect that would otherwise retry in silence.
                    if (e is StorageFullException || e is QueueSubmitFailed || e is SQLiteException) {
                        Log.w(TAG, "Automatic downloads of ${store.account} failed", e)
                    } else {
                        Log.e(TAG, "Automatic downloads of ${store.account} failed unexpectedly", e)
                    }
                    arm(store, request?.replace ?: false, fresh = request != null)
                }
            }
        } finally {
            job?.let(following::remove)
        }
    }

    /**
     * The background drain's step (P2 §17): reconciles and schedules the owner whose directory is [accountDir],
     * waiting for its start. True when done; false asks for a retry (a refused commit or a failed submission).
     */
    suspend fun settle(accountDir: String): Boolean {
        // Another account's open owner is nothing to wait for; none open yet is.
        val store = withTimeoutOrNull(ownerWait) { owners.first { it != null } }?.takeIf { it.accountStore.dir.name == accountDir } ?: return true
        return try {
            reconcile(store, null)
            schedule(store, false, store.wakes.value)
            true
        } catch (e: SyncUnavailable) {
            true
        } catch (e: StorageFullException) {
            false
        } catch (e: QueueSubmitFailed) {
            false
        }
    }

    /** The account's store is closing: every [follow] is cancelled and joined. */
    suspend fun stop() {
        for (job in following.toList()) job.cancelAndJoin()
    }

    companion object {
        const val RETRY_MS = 30_000L
        const val OWNER_WAIT_MS = 10_000L
        const val SAMPLE_MS = 300L
        private const val TAG = "AutoDownloads"
    }
}
