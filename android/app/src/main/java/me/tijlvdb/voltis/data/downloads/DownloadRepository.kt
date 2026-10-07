package me.tijlvdb.voltis.data.downloads

import android.content.Context
import android.net.ConnectivityManager
import android.net.Uri
import android.os.StatFs
import android.os.SystemClock
import android.util.Log
import androidx.work.BackoffPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkInfo
import androidx.work.WorkManager
import androidx.work.await
import androidx.work.workDataOf
import dagger.hilt.android.qualifiers.ApplicationContext
import java.io.File
import java.util.concurrent.TimeUnit
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.onStart
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.auth.AppScope
import me.tijlvdb.voltis.data.content.ContentListParams
import me.tijlvdb.voltis.data.db.AccountScoped
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.data.db.DownloadRow
import me.tijlvdb.voltis.data.net.anyNetwork
import me.tijlvdb.voltis.data.settings.DeviceSettings
import me.tijlvdb.voltis.data.storage.localTransaction
import me.tijlvdb.voltis.domain.downloads.DownloadBadge
import me.tijlvdb.voltis.domain.downloads.RequestedBy
import me.tijlvdb.voltis.domain.downloads.SeriesPolicy
import me.tijlvdb.voltis.domain.downloads.downloadBadge
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.reading.ReadingSync
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.storage.StorageFullException

/**
 * The open account's downloads (P2 §8): its [DownloadStore], which owns the rows and files, the
 * cached content rows, and the unique work `downloads` that runs the queue. Each call resolves
 * the owner open at that moment once.
 */
@OptIn(ExperimentalCoroutinesApi::class)
/** One open account's download rows with the bytes held by detached directories, the series with a policy, and the free bytes. */
class DownloadSnapshot(val account: String, val rows: List<DownloadRow>, val detached: Long, val policies: Set<String>, val free: Long)

@Singleton
class DownloadRepository @Inject constructor(
    @ApplicationContext private val context: Context,
    private val stores: AccountStores,
    private val api: VoltisApi,
    private val reading: ReadingSync,
    private val connectivity: Connectivity,
    private val settings: DeviceSettings,
    @AppScope private val scope: CoroutineScope,
) : AccountScoped {
    // Only once the application has its worker factory: see begin().
    private val work by lazy { WorkManager.getInstance(context) }
    private val owners = DownloadOwners(stores.current, scope)

    /** The store open by the account stores, which `current` follows with a delay. */
    private fun open(): DownloadStore? = current.value?.takeIf { stores.current.value === it.accountStore }

    private val starter = QueueStarter(
        ::open,
        { settings.downloadWifiOnly.first() },
        { work.getWorkInfosForUniqueWorkFlow(WORK).first().any { it.state == WorkInfo.State.RUNNING } },
        { store, wifiOnly, append ->
            val request = OneTimeWorkRequestBuilder<DownloadWorker>()
                .setConstraints(anyNetwork(unmetered = wifiOnly))
                .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS)
                .setInputData(workDataOf(DownloadWorker.ACCOUNT to store.accountStore.dir.name))
                .build()
            // Returns once WorkManager has stored the request.
            work.enqueueUniqueWork(WORK, if (append) ExistingWorkPolicy.APPEND_OR_REPLACE else ExistingWorkPolicy.REPLACE, request).await()
            Unit
        },
        { work.cancelUniqueWork(WORK) },
    )

    private val auto = AutoDownloads(
        { current.value },
        owners.current,
        schedule = { store, replace, wake -> starter.ensureScheduled(store, replace, wake) },
        clock = SystemClock::elapsedRealtime,
    )

    private val bulk = BulkQueue(
        ::open,
        { store, rows -> cacheForQueue(store, api, rows, { r -> reading.seed(store.account, r) }, System::currentTimeMillis) },
        { store -> ensureScheduled(store) },
    )

    init {
        stores.register(this)
    }

    /** The open account's downloads; null while no store is open. */
    val current: StateFlow<DownloadStore?> = owners.current

    /** The account whose offline data couldn't be opened ("Couldn't open offline data"): its database, or its downloads. */
    val failed: StateFlow<String?> = combine(
        stores.failed,
        current.flatMapLatest { store -> store?.failed?.map { if (it) store.account else null } ?: flowOf(null) },
    ) { db, downloads -> db ?: downloads }.stateIn(scope, SharingStarted.Eagerly, null)

    /** Opens the account's database again after it failed: the disk may have room now. */
    fun retryOpen() {
        scope.launch { stores.retry() }
    }

    /** The open account's policies by series. */
    val policies: Flow<Map<String, SeriesPolicy>> = current
        .flatMapLatest { it?.accountStore?.db?.auto()?.observePolicies()?.closedAs(emptyList()) ?: flowOf(emptyList()) }
        .map { rows -> rows.associate { it.seriesId to SeriesPolicy(it.seriesId, it.keepNext, it.deleteFinished) } }
        .distinctUntilChanged()

    val rows: Flow<List<DownloadRow>> = current.flatMapLatest { it?.accountStore?.db?.downloads()?.rows()?.closedAs(emptyList()) ?: flowOf(emptyList()) }

    /**
     * What a screen about the open account's downloads shows, from one store: null until that store's rows,
     * policies and sizes are read, and again whenever the store changes (none open is unknown, not empty).
     */
    fun snapshot(): Flow<DownloadSnapshot?> = current.flatMapLatest { store ->
        val db = store?.accountStore?.db ?: return@flatMapLatest flowOf(null)
        // Nothing is read before the store's startup (trash wipe, copy validation, leftover scan) is done.
        store.started.flatMapLatest { started ->
            if (!started) return@flatMapLatest flowOf(null)
            combine(db.downloads().rows().closedAs(emptyList()), store.detachedBytes, db.auto().observePolicies().closedAs(emptyList())) { rows, detached, policies ->
                DownloadSnapshot(store.account, rows, detached, policies.mapTo(HashSet()) { it.seriesId }, freeBytes(store.accountStore.dir))
            }.flowOn(Dispatchers.IO)
        }.onStart<DownloadSnapshot?> { emit(null) }
    }

    /** Directories on disk that no row counts: replaced copies a reader holds, transfers still ending, the trash not yet wiped. */
    val detachedBytes: Flow<Long> = current.flatMapLatest { it?.detachedBytes ?: flowOf(0L) }

    /** Rows queued or running. */
    val active: Flow<Int> = current.flatMapLatest { it?.accountStore?.db?.downloads()?.active()?.closedAs(0) ?: flowOf(0) }

    fun download(contentId: String): Flow<DownloadEntity?> =
        current.flatMapLatest { it?.accountStore?.db?.downloads()?.observe(contentId)?.closedAs(null) ?: flowOf(null) }

    /**
     * The cards' marks by content ID (P2 §11), from one DAO flow shared by every screen: each item's
     * own; a series has the downloaded mark when any volume has a copy.
     */
    val badges: StateFlow<Map<String, DownloadBadge>> = current
        .flatMapLatest { it?.accountStore?.db?.downloads()?.marks()?.closedAs(emptyList()) ?: flowOf(emptyList()) }
        .map { rows ->
            val items = rows.associate { it.contentId to downloadBadge(it.state, it.copyId != null, it.stale) }
            val series = rows.filter { it.seriesId != it.contentId && it.copyId != null }.associate { it.seriesId to DownloadBadge.DOWNLOADED }
            series + items
        }
        .flowOn(Dispatchers.Default)
        .distinctUntilChanged()
        .stateIn(scope, SharingStarted.WhileSubscribed(5_000), emptyMap())

    /** A series' rows, for its "N of M downloaded" line. */
    fun series(seriesId: String): Flow<List<DownloadEntity>> =
        current.flatMapLatest { it?.accountStore?.db?.downloads()?.series(seriesId)?.closedAs(emptyList()) ?: flowOf(emptyList()) }

    /** Free bytes where downloads are stored. */
    suspend fun freeBytes(): Long? = withContext(Dispatchers.IO) { current.value?.accountStore?.dir?.let(::freeBytes) }

    /**
     * Called once the application is injected (WorkManager needs its worker factory): restarts the
     * queue whenever an account's downloads open, and when the server comes back or the Wi-Fi
     * setting changes.
     */
    fun begin() {
        scope.launch { current.collect { store -> if (store != null && hasActive(store)) startOrRetry(store, replace = false) } }
        scope.launch { connectivity.online.drop(1).filter { it }.collect { current.value?.takeIf { hasActive(it) }?.let { startOrRetry(it, replace = false) } } }
        scope.launch { settings.downloadWifiOnly.drop(1).collect { current.value?.takeIf { hasActive(it) }?.let { startOrRetry(it, replace = true) } } }
        scope.launch {
            current.collectLatest { store ->
                auto.ownerChanged()
                if (store != null) {
                    try {
                        auto.follow(store)
                    } catch (e: SyncUnavailable) {
                        Log.i(TAG, "Stopped following the automatic downloads", e)
                    }
                }
            }
        }
    }

    /** An automatic start: a failed submission is logged and tried again later, and the collector goes on. */
    private suspend fun startOrRetry(store: DownloadStore, replace: Boolean) {
        try {
            start(replace, store)
        } catch (e: QueueSubmitFailed) {
            Log.w(TAG, "Couldn't start the downloads", e)
            auto.retrySoon(store, replace)
        }
    }

    private suspend fun hasActive(store: DownloadStore) = try {
        store.accountStore.db.downloads().activeNow() > 0
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        Log.e(TAG, "Couldn't read the queue", e)
        false
    }

    /**
     * Runs the queue now (P2 §8). A running worker gets this run appended, so a row it already
     * passed is still taken; otherwise the work is replaced, which also cuts a retry's backoff
     * short. [replace] replaces a running one too (new constraints). With an [owner], only while it is the open one.
     */
    suspend fun start(replace: Boolean = false, owner: DownloadStore? = null) = starter.start(replace, owner)

    /**
     * Queues [items], comic volumes or standalone comics, as content rows the caller has (P2 §8,
     * Queue): each series is cached once, the lanes are seeded, and rows go in in chunks.
     */
    suspend fun enqueue(items: List<Content>, requestedBy: String = RequestedBy.USER) {
        val store = current.value ?: throw if (failed.value != null) SyncUnavailable.OfflineData() else SyncUnavailable.AccountChanged()
        enqueueDownloads(
            store, api, items, requestedBy, { rows -> reading.seed(store.account, rows) },
            now = System::currentTimeMillis, schedule = { ensureScheduled(store) },
        )
    }

    /**
     * Sets or removes [seriesId]'s policy for [owner] and applies it (P2 §17). A policy first fetches and
     * caches the series, so a first save on a series never downloaded queues the window at once; a failed fetch
     * throws and saves nothing. Null when [owner] isn't the open store, at entry or after the fetch.
     */
    suspend fun setPolicy(owner: DownloadStore, seriesId: String, policy: SeriesPolicy?): AutoApplied? {
        if (!isCurrent(owner)) return null
        // Until the owner command has consumed the cache, no prune takes it.
        // A removal has no cache to protect, and must prune its own series.
        val held = if (policy != null) owner.reserve(listOf(seriesId)) else null
        try {
            if (policy != null) {
                cacheSeries(owner, api, seriesId, { rows -> reading.seed(owner.account, rows) }, System::currentTimeMillis)
                if (!isCurrent(owner)) return null
            }
            val applied = owner.setPolicy(seriesId, policy)
            ensureScheduled(owner)
            return applied.takeIf { isCurrent(owner) }
        } catch (e: SyncUnavailable.AccountChanged) {
            return null
        } finally {
            held?.release()
        }
    }

    /** The finished volumes "delete after finishing" would delete from [seriesId] now (the sheet's line). */
    suspend fun finishedDeletable(owner: DownloadStore, seriesId: String): Int = try {
        owner.finishedDeletable(seriesId)
    } catch (e: SyncUnavailable) {
        0
    }

    /** The background drain's step: false asks for a retry. See [AutoDownloads.settle]. */
    suspend fun settle(accountDir: String): Boolean = auto.settle(accountDir)

    /**
     * A bulk download (P4 §6) for [owner], the downloads the dialog counted for. Another owner open now,
     * or none, is nothing: null. Otherwise the content rows of what is still missing are cached, and
     * [DownloadStore.queueBulk] reconciles [items] against the rows as they are, in one commit; the
     * owner's queue is scheduled once when the call is accepted, queued rows or not.
     */
    suspend fun queueBulk(owner: DownloadStore, items: List<Content>): BulkQueued? = bulk.queue(owner, items)

    /** [owner] is the open store, by the account stores and not only the derived flow. */
    fun isCurrent(owner: DownloadStore) = owner.isOpen && stores.current.value === owner.accountStore

    /** Whether a download started now would wait: Wi-Fi only is on and the network is metered. */
    suspend fun waitsForWifi(): Boolean =
        settings.downloadWifiOnly.first() && context.getSystemService(ConnectivityManager::class.java).isActiveNetworkMetered

    suspend fun pause(id: String) = control { pause(id) }

    suspend fun resume(id: String) = wake(control { resume(id) })

    suspend fun pauseAll() = control { pauseAll() }

    /** Paused rows and those that failed for storage. */
    suspend fun resumeAll() = wake(control { resumeAll() })

    /** Also "Download again" for a copy whose files went missing. */
    suspend fun retry(id: String) = wake(control { retry(id) })

    /** A download updated on the server is fetched anew; its copy stays readable until the new one is complete. */
    suspend fun downloadAgain(id: String) = wake(control { again(id) })

    /** A queued, running or failed item goes, with its files; a re-download goes back to its copy. */
    suspend fun cancel(id: String) = control { cancel(id) }

    suspend fun delete(ids: List<String>) = control { delete(ids) }

    suspend fun deleteAll() = control { deleteAll() }

    /**
     * The notification's Pause and Cancel (`voltis-download://<accountDir>/<contentId>/<transferId>/<action>`):
     * only for that account while it is open, and only for the transfer the notification was posted for.
     */
    suspend fun fromNotification(uri: Uri) {
        val action = DownloadAction.parse(uri) ?: return
        val store = current.value?.takeIf { it.accountStore.dir.name == action.accountDir } ?: return
        try {
            if (action.cancel) store.cancel(action.contentId, action.transferId) else store.pause(action.contentId, action.transferId)
        } catch (e: SyncUnavailable) {
            Log.i(TAG, "Dropped a notification action", e)
        } catch (e: StorageFullException) {
            // Refused and unapplied; the transaction wrapper has raised the storage alert.
            Log.w(TAG, "Dropped a notification action: storage is full", e)
        }
    }

    /** The engine's hook (P2 §5, Results): the server answered 404 for [contentId] of [account], to a request sent at [at]. */
    suspend fun markGone(account: String, contentId: String, at: Long) {
        val store = current.value?.takeIf { it.account == account } ?: return
        try {
            store.markGone(contentId, at)
        } catch (e: SyncUnavailable) {
            Log.i(TAG, "Dropped a gone mark", e)
        } catch (e: StorageFullException) {
            Log.w(TAG, "Dropped a gone mark: storage is full", e)
        }
    }

    /** The account's store is closing: its work is cancelled and its downloads stopped. */
    override suspend fun stop() {
        try {
            starter.stop()
        } finally {
            try {
                auto.stop()
            } finally {
                owners.stop()
            }
        }
    }

    /**
     * On the owner open now; nothing when none is, or it closed meanwhile. Returns the owner it ran on, else null.
     * A refused commit (StorageFullException) reaches the caller, who shows it: the change wasn't made.
     */
    private suspend fun control(block: suspend DownloadStore.() -> Unit): DownloadStore? {
        val store = current.value ?: return null
        return try {
            store.block()
            store
        } catch (e: SyncUnavailable) {
            Log.i(TAG, "Downloads aren't open", e)
            null
        }
    }

    /** After a control that made rows runnable: starts the queue now. The owner's follower also schedules from the commit's wake; whichever submits first records it as serviced, so one control submits once. */
    private suspend fun wake(store: DownloadStore?) {
        store ?: return
        try {
            starter.start(owner = store, wake = store.wakes.value)
        } catch (e: QueueSubmitFailed) {
            auto.retrySoon(store, false)
            throw e
        }
    }

    /** [QueueStarter.ensureScheduled] for [store]; a failed submission is retried later, and still reaches the caller. */
    private suspend fun ensureScheduled(store: DownloadStore) {
        try {
            starter.ensureScheduled(store, wake = store.wakes.value)
        } catch (e: QueueSubmitFailed) {
            auto.retrySoon(store, false)
            throw e
        }
    }

    companion object {
        const val WORK = "downloads"
        private const val TAG = "Downloads"

        fun freeBytes(dir: File): Long = StatFs(dir.path).availableBytes
    }
}

/** A database flow that fails (the store closing under an account switch) ends as [empty], as [DownloadStore.changes] does. */
internal fun <T> Flow<T>.closedAs(empty: T): Flow<T> = catch { e ->
    Log.i("Downloads", "A database flow ended", e)
    emit(empty)
}

/**
 * One [DownloadStore] for the account store open now: created only for the store [stores] holds
 * at that moment, and stopped before that store closes. [mapping] runs before each store is mapped to its owner (tests).
 */
internal class DownloadOwners(
    private val stores: StateFlow<AccountStore?>,
    scope: CoroutineScope,
    private val create: (AccountStore, (PinKey) -> Unit) -> DownloadStore = { store, onFree -> DownloadStore(store, scope, onFree) },
    private val mapping: suspend (AccountStore?) -> Unit = {},
) {
    /** Guarded by `this`. */
    private var owned: DownloadStore? = null

    val current: StateFlow<DownloadStore?> = stores.map {
        mapping(it)
        it?.let(::ownerFor)
    }.stateIn(scope, SharingStarted.Eagerly, null)

    private fun ownerFor(store: AccountStore): DownloadStore? = synchronized(this) {
        // A late emission: that store is closing or closed, and stop() has taken (or will take) its owner.
        if (stores.value !== store) return null
        owned?.takeIf { it.accountStore === store } ?: run {
            owned?.let { Log.e(TAG, "The owner of a previous store was never stopped") }
            create(store, ::freed).also { owned = it }
        }
    }

    /** A pin's last hold went: its directory is collected if its account is open, else at that account's next start. */
    private fun freed(key: PinKey) {
        synchronized(this) { owned }?.takeIf { it.accountStore.dir.name == key.accountDir }?.collectSoon(key.dirId)
    }

    suspend fun stop() {
        synchronized(this) { owned.also { owned = null } }?.stop()
    }

    private companion object {
        const val TAG = "DownloadOwners"
    }
}

/** A notification action's identity: its data URI, `voltis-download://<accountDir>/<contentId>/<transferId>/<action>`. */
internal data class DownloadAction(val accountDir: String, val contentId: String, val transferId: String, val cancel: Boolean) {
    fun uri(): Uri = Uri.Builder().scheme(SCHEME).authority(accountDir)
        .appendPath(contentId).appendPath(transferId).appendPath(if (cancel) CANCEL else PAUSE).build()

    companion object {
        private const val SCHEME = "voltis-download"
        private const val PAUSE = "pause"
        private const val CANCEL = "cancel"

        fun parse(uri: Uri): DownloadAction? {
            val path = uri.pathSegments
            if (uri.scheme != SCHEME || path.size != 3 || path[2] !in setOf(PAUSE, CANCEL)) return null
            return DownloadAction(uri.authority ?: return null, path[0], path[1], path[2] == CANCEL)
        }
    }
}

/**
 * [DownloadRepository.enqueue], without Android: per series one request for its row and one for
 * its list (the cached volumes); standalone items, and a series that couldn't be fetched, cached as
 * passed; lanes seeded without page fields ([forSeed]); queued rows in chunks of 100, and [schedule]
 * once, after the last: a failure to schedule reaches the caller with every row in.
 */
internal suspend fun enqueueDownloads(
    store: DownloadStore,
    api: VoltisApi,
    items: List<Content>,
    requestedBy: String,
    seed: suspend (List<Content>) -> Unit,
    now: () -> Long,
    schedule: suspend () -> Unit,
) {
    val held = store.reserve(items.map { it.parentId ?: it.id })
    try {
        cacheForQueue(store, api, items, seed, now)
        for (chunk in items.chunked(100)) store.enqueue(chunk.map { NewDownload(it.id, it.parentId ?: it.id, requestedBy) })
    } finally {
        held.release()
    }
    schedule()
}

/** The content rows [items] are queued from: each series cached once, standalone items as passed, lanes seeded. */
internal suspend fun cacheForQueue(
    store: DownloadStore,
    api: VoltisApi,
    items: List<Content>,
    seed: suspend (List<Content>) -> Unit,
    now: () -> Long,
) {
    val db = store.accountStore.db

    /** The rows as passed: their reading states carry their seq, so they update nothing that is newer. */
    suspend fun cachePassed(group: List<Content>) {
        db.cache(group.map { it to null }, now()) { it.isDetail }
        seed(group.map { it.forSeed() })
    }
    for ((seriesId, group) in items.groupBy { it.parentId }) {
        if (seriesId == null) {
            cachePassed(group)
            continue
        }
        val fetched = try {
            fetchSeries(store, api, seriesId, now)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // No server: queued from the rows as passed; the worker fetches each detail row.
            null
        }
        if (fetched == null) {
            cachePassed(group)
            continue
        }
        db.cache(group.filter { it.isDetail }.map { it to fetched.positions[it.id] }, fetched.fetchedAt) { true }
        val wanted = group.map { it.id }.toSet()
        seed((listOf(fetched.series) + fetched.volumes.filter { it.id in wanted }).map { it.forSeed() })
    }
}

/** A series' row and its whole list as the server answered. */
internal class SeriesFetch(val series: Content, val volumes: List<Content>, val fetchedAt: Long) {
    val positions = volumes.withIndex().associate { (i, v) -> v.id to i }
}

/** Fetches [seriesId]'s row and list (every type, in the server's order) and caches both; throws when either request fails. */
private suspend fun fetchSeries(store: DownloadStore, api: VoltisApi, seriesId: String, now: () -> Long): SeriesFetch {
    val account = ForAccount(store.account)
    val series = api.contentById(seriesId, account)
    val volumes = api.content(ContentListParams.volumes(seriesId).toQuery(), account).data
    val fetched = SeriesFetch(series, volumes, now())
    val db = store.accountStore.db
    db.localTransaction {
        db.cache(listOf(series to null), fetched.fetchedAt) { true }
        db.cache(volumes.map { it to fetched.positions[it.id] }, fetched.fetchedAt) { false }
        db.content().setVolumesKnown(seriesId, true)
        // The list is complete: children gone from it go, or stay invalid while downloaded or unsent (as in refreshCatalog).
        (db.content().childIds(seriesId) - fetched.positions.keys).chunked(500).forEach {
            db.content().deleteUnneeded(it)
            db.content().invalidate(it)
        }
    }
    return fetched
}

/**
 * Caches [seriesId]'s row and its whole list for [store]'s account and seeds every listed volume (P2 §17,
 * Bootstrap on save), so a policy's first save sees the series as it is. Throws when the server can't be reached.
 */
internal suspend fun cacheSeries(
    store: DownloadStore,
    api: VoltisApi,
    seriesId: String,
    seed: suspend (List<Content>) -> Unit,
    now: () -> Long,
) {
    val fetched = fetchSeries(store, api, seriesId, now)
    seed((listOf(fetched.series) + fetched.volumes).map { it.forSeed() })
}
