package me.tijlvdb.voltis.data.downloads

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.domain.catalog.MAX_BULK_DOWNLOADS

/** WorkManager didn't store the request for the queue's work; the downloads are in the rows, and nothing runs them yet. */
class QueueSubmitFailed(cause: Throwable) : Exception("Couldn't start the downloads", cause)

/**
 * Runs the download work (P2 §8), without Android: [start] and [stop] share one lock, and a start
 * checks its store again after every suspension, so none enqueues after teardown has cancelled.
 */
internal class QueueStarter(
    /** The open store, from the authoritative account stores. */
    private val current: () -> DownloadStore?,
    private val wifiOnly: suspend () -> Boolean,
    private val isRunning: suspend () -> Boolean,
    /** Returns once the request is stored. */
    private val enqueue: suspend (store: DownloadStore, wifiOnly: Boolean, append: Boolean) -> Unit,
    private val cancel: () -> Unit,
) {
    private val lock = Mutex()

    /** With an [owner], only while it is the open store, also after the lookups. */
    suspend fun start(replace: Boolean = false, owner: DownloadStore? = null, wake: Long? = null) = lock.withLock {
        fun open() = current()?.takeIf { (owner == null || it === owner) && it.isOpen }
        try {
            open() ?: return@withLock
            val wifi = wifiOnly()
            val running = isRunning()
            val store = open() ?: return@withLock
            // A wake whose work another caller already submitted is done; a replacement is wanted all the same.
            if (!replace && wake != null && wake > 0 && store.servicedWakes >= wake) return@withLock
            enqueue(store, wifi, running && !replace)
            wake?.let(store::service)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // The settings and WorkManager's database aren't the account's: their failures, a full disk too, are no StorageFullException.
            throw QueueSubmitFailed(e)
        }
    }

    /** Starts the work when [store] has runnable rows, whoever queued them (P2 §17). */
    suspend fun ensureScheduled(store: DownloadStore, replace: Boolean = false, wake: Long? = null) {
        if (store.accountStore.db.downloads().activeNow() > 0) start(replace, store, wake)
    }

    /** After this returns, no start enqueues for the store that closed. */
    suspend fun stop() = lock.withLock { cancel() }
}

/** A bulk download's steps, with the store checks between them. */
internal class BulkQueue(
    private val current: () -> DownloadStore?,
    private val cache: suspend (DownloadStore, List<Content>) -> Unit,
    private val start: suspend (DownloadStore) -> Unit,
) {
    private fun open(owner: DownloadStore) = owner.isOpen && current() === owner

    /** Null when [owner] stopped being the open store at any point: nothing is reported for it. */
    suspend fun queue(owner: DownloadStore, items: List<Content>): BulkQueued? {
        if (!open(owner)) return null
        // Until the queueBulk command has consumed the cache, no prune takes it.
        val held = owner.reserve(items.map { it.parentId ?: it.id })
        try {
            val present = owner.presentIds()
            cache(owner, items.filter { it.id !in present })
            if (!open(owner)) return null
            val result = owner.queueBulk(items.map { NewDownload(it.id, it.parentId ?: it.id, me.tijlvdb.voltis.domain.downloads.RequestedBy.USER) }, MAX_BULK_DOWNLOADS)
            // Also when nothing new was queued: an earlier call may have committed its rows and failed to submit.
            if (!result.tooMany) start(owner)
            return result.takeIf { open(owner) }
        } catch (e: me.tijlvdb.voltis.domain.reading.SyncUnavailable.AccountChanged) {
            return null
        } catch (e: Exception) {
            if (!open(owner)) return null
            throw e
        } finally {
            held.release()
        }
    }
}
