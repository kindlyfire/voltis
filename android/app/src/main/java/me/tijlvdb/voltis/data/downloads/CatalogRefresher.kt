package me.tijlvdb.voltis.data.downloads

import android.os.SystemClock
import android.util.Log
import me.tijlvdb.voltis.data.storage.localTransaction
import java.util.concurrent.ConcurrentHashMap
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withTimeoutOrNull
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.auth.AppScope
import me.tijlvdb.voltis.data.content.ContentListParams
import me.tijlvdb.voltis.data.db.AccountScoped
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.data.net.isContentMissing
import me.tijlvdb.voltis.data.net.isUnreachable
import me.tijlvdb.voltis.domain.downloads.ServerFile
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.reading.ReadingSync
import retrofit2.HttpException

/**
 * Keeps the cached rows of downloaded items and their series current, seeds their lanes, and marks
 * stale downloads (P2 §8, Stale detection). Automatic passes (the app coming to the foreground,
 * the outbox draining) run online and at most every 15 minutes per account; the queue emptying and
 * pull-to-refresh in Downloads always run.
 */
@Singleton
class CatalogRefresher @Inject constructor(
    private val stores: AccountStores,
    private val api: VoltisApi,
    private val reading: ReadingSync,
    private val connectivity: Connectivity,
    private val downloads: DownloadRepository,
    @AppScope private val scope: CoroutineScope,
) : AccountScoped {
    private val lock = Mutex()
    private val running = mutableSetOf<Job>()

    /** By account, on the `elapsed` clock: when the last pass that reached the server started. */
    private val lastRun = ConcurrentHashMap<String, Long>()

    init {
        stores.register(this)
    }

    /** From the application's start: the triggers that follow the outbox and the queue. */
    fun begin() {
        scope.launch { reading.unsent.map { it > 0 }.distinctUntilChanged().drop(1).filter { !it }.collect { refresh(automatic = true) } }
        scope.launch { downloads.active.map { it > 0 }.distinctUntilChanged().drop(1).filter { !it }.collect { refresh(automatic = false) } }
    }

    /**
     * A pass over the open account's downloads, in a coroutine of its own (so a store's close,
     * which cancels it, doesn't reach the caller), one at a time. An [automatic] one runs only
     * online and 15 minutes after the account's last pass that reached the server. Failures leave
     * what is cached.
     */
    fun refresh(automatic: Boolean): Job {
        val job = scope.launch(start = CoroutineStart.LAZY) {
            try {
                lock.withLock { pass(automatic) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Log.e(TAG, "Couldn't refresh the downloads' rows", e)
            }
        }
        synchronized(running) { running += job }
        job.invokeOnCompletion { synchronized(running) { running -= job } }
        job.start()
        return job
    }

    private suspend fun pass(automatic: Boolean) {
        // At process start the store opens a moment after the app comes to the foreground.
        val store = withTimeoutOrNull(STORE_WAIT) { downloads.current.filterNotNull().first() } ?: return
        val started = SystemClock.elapsedRealtime()
        if (automatic) {
            val last = lastRun[store.account]
            if (!connectivity.online.value || (last != null && started - last < INTERVAL)) return
        }
        val reached = refreshCatalog(store, api, { rows -> reading.seed(store.account, rows) }, System::currentTimeMillis)
        // A pass that reached nothing doesn't hold the next one back.
        if (reached) lastRun[store.account] = started
    }

    /** The account's store is closing: a pass under way is cancelled and waited for, and the next account starts unthrottled. */
    override suspend fun stop() {
        try {
            synchronized(running) { running.toList() }.forEach { it.cancelAndJoin() }
        } finally {
            lastRun.clear()
        }
    }

    private companion object {
        const val TAG = "CatalogRefresher"
        const val INTERVAL = 15 * 60_000L
        const val STORE_WAIT = 10_000L
    }
}

/**
 * [CatalogRefresher.refresh], without Android: per series with downloads or a policy its row and its list, per
 * standalone item its row. Each answer is applied in one transaction, and only if the series still
 * has downloads or unsent ops: the cached rows are updated (a list row never replaces a detail row's
 * `json`), rows gone from a list are deleted, or kept invalid while downloaded or with unsent ops.
 * Then the store takes what the answer said of each download row, as of when its request started,
 * and the series and its downloaded items are seeded without page fields. Their reading states are
 * imported into the snapshot table with the rows. A series or item that
 * can't be fetched is left as it is. True when at least one answer was Voltis'.
 */
internal suspend fun refreshCatalog(
    store: DownloadStore,
    api: VoltisApi,
    seed: suspend (List<Content>) -> Unit,
    now: () -> Long,
): Boolean {
    val db = store.accountStore.db
    val account = ForAccount(store.account)
    var reached = false
    // A series with a policy is refreshed without downloads too: its window needs the list (P2 §17).
    val groups = db.downloads().all().groupBy { it.seriesId }
    val series = groups + db.auto().policies().map { it.seriesId }.filter { it !in groups }.associateWith { emptyList() }
    for ((seriesId, rows) in series) {
        val standalone = rows.size == 1 && rows[0].contentId == seriesId
        var at = now()
        val (series, listed) = try {
            // Each answer counts as reaching the server, also when the series' list then fails.
            val series = fetchOrMissing { api.contentById(seriesId, account) }.also { reached = true }
            series to when {
                standalone -> listOfNotNull(series)
                // A series that is gone lists nothing: its volumes are gone with it.
                series == null -> emptyList()
                else -> {
                    // The volumes are as the list request saw them, not the series' request before it.
                    at = now()
                    api.content(ContentListParams.volumes(seriesId).toQuery(), account).data
                }
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // A Voltis 4xx is an answer all the same.
            if (e is HttpException && !e.isUnreachable()) reached = true
            continue
        }
        val fetchedAt = now()
        val byId = listed.associateBy { it.id }
        val current = db.localTransaction {
            // Deleted while the request was out: nothing needs these rows any more.
            val current = db.downloads().inSeries(seriesId)
            if (current.isEmpty() && !db.ops().unsentIn(seriesId) && db.auto().policy(seriesId) == null) return@localTransaction null
            series?.let { db.cache(listOf(it to null), fetchedAt) { true } }
            if (!standalone) {
                val positions = listed.withIndex().associate { (i, v) -> v.id to i }
                db.cache(listed.map { it to positions[it.id] }, fetchedAt) { false }
                // The list is whole: the series' membership is known, empty included.
                if (series != null) db.content().setVolumesKnown(seriesId, true)
                val missing = db.content().childIds(seriesId) - positions.keys
                missing.chunked(500).forEach {
                    db.content().deleteUnneeded(it)
                    db.content().invalidate(it)
                }
            }
            current
        } ?: continue
        if (current.isNotEmpty()) store.observe(current.map { row -> Observation(row.contentId, byId[row.contentId]?.let { ServerFile(it.valid, it.fileMtime, it.fileSize) }, at) })
        // A policy series seeds every listed volume: the window reads a lane's status before the cached row's.
        val wanted = if (db.auto().policy(seriesId) != null) listed.mapTo(HashSet()) { it.id } else current.map { it.contentId }.toSet()
        seed((listOfNotNull(series.takeIf { !standalone }) + listed.filter { it.id in wanted }).map { it.forSeed() })
    }
    return reached
}

/** The answer, or null for Voltis' "Content not found". */
private suspend fun fetchOrMissing(get: suspend () -> Content): Content? = try {
    get()
} catch (e: Exception) {
    if (e is CancellationException || !e.isContentMissing()) throw e
    null
}
