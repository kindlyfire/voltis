package me.tijlvdb.voltis.data.sync

import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.onStart
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.json.JsonElement
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.auth.AppScope
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.db.AccountScoped
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.data.db.OpenAccount
import me.tijlvdb.voltis.data.reading.SyncWork
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.reading.AttentionItem
import me.tijlvdb.voltis.domain.reading.DrainResult
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.domain.sync.PendingUserData
import me.tijlvdb.voltis.domain.sync.UserDataItem
import me.tijlvdb.voltis.domain.sync.UserDataSync

/**
 * The pending star and rating rows (P4 §8): one [PendingOwner] per account store. The collector of
 * `AccountStores.current` only creates an owner, under [lock] and while its store is still the current one;
 * [stop], which `AccountStores` calls after `current` went null and before the database closes, is the only
 * thing that clears it, under the same lock. Each public call resolves its owner once ([ownerOf]) and works only on it.
 */
@OptIn(ExperimentalCoroutinesApi::class)
@Singleton
class PendingSync internal constructor(
    private val current: StateFlow<OpenAccount?>,
    /** The signed-in account, read from the session itself. */
    private val signedIn: () -> String?,
    /** Emits whenever [signedIn] may have changed. */
    private val sessionChanges: Flow<*>,
    /** The signed-in account whose store couldn't be opened. */
    private val failed: StateFlow<String?>,
    private val newOwner: (OpenAccount, foreground: () -> Boolean) -> PendingOwner,
    /** Before an owner stops: the account's background work is cancelled and no longer scheduled. */
    private val closeWork: (account: String) -> Unit,
    private val scope: CoroutineScope,
) : UserDataSync, AccountScoped {
    @Inject constructor(
        stores: AccountStores,
        session: SessionStore,
        api: VoltisApi,
        events: CatalogEvents,
        notices: RoomSyncNotices,
        connectivity: Connectivity,
        work: SyncWork,
        @AppScope scope: CoroutineScope,
    ) : this(
        stores.current,
        { session.state.value.account },
        session.state,
        stores.failed,
        { open, foreground ->
            val store = open as AccountStore
            PendingOwner(
                store.account, RoomPendingStore(store.db), RetrofitPendingTransport(api, store.account), connectivity,
                work.open(store.dir.name), notices::announce, events,
                foreground = foreground,
            )
        },
        { work.close(AccountStores.directoryName(it)) },
        scope,
    ) {
        stores.register(this)
    }

    private class Bound(val store: OpenAccount, val owner: PendingOwner) {
        val account get() = store.account
    }

    private val bound = MutableStateFlow<Bound?>(null)

    /** Creation and clearing of [bound]: a late start can't publish an owner after [stop] cleared the one before it. */
    private val lock = Mutex()

    init {
        // A store appears only after the previous one's stop() returned (AccountStores.switchTo).
        scope.launch { current.collect { store -> store?.let { start(it) } } }
    }

    /** Whether the app is in the foreground (`ON_START` to `ON_STOP`): owners read it live, and [foreground] can come before one is bound. */
    @Volatile
    private var inForeground = false

    private suspend fun start(store: OpenAccount) = lock.withLock {
        if (bound.value != null || current.value !== store) return@withLock
        val owner = newOwner(store) { inForeground }
        try {
            owner.start()
        } catch (e: CancellationException) {
            owner.stop()
            throw e
        }
        bound.value = Bound(store, owner)
        // A foreground() before this publish found no owner: the flag is set before it looks, so it is seen here.
        // A background() meanwhile needs nothing: the owner reads the flag live.
        if (inForeground) {
            owner.request(RunKind.FOREGROUND)
        }
    }

    override suspend fun stop() = lock.withLock {
        bound.value?.let {
            bound.value = null
            // Before the owner stops: a worker waiting on its drain is cancelled, and nothing schedules another.
            closeWork(it.account)
            it.owner.stop()
        }
        Unit
    }

    /** [account]'s owner once it exists; fails when its store can't be opened, or [account] is no longer signed in. */
    private suspend fun ownerOf(account: String): PendingOwner =
        combine(bound, failed, sessionChanges) { bound, failed, _ ->
            when {
                signedIn() != account -> throw SyncUnavailable.AccountChanged()
                bound?.account == account -> bound.owner
                failed == account -> throw SyncUnavailable.OfflineData()
                else -> null
            }
        }.filterNotNull().first()

    override suspend fun setUserData(account: String, item: UserDataItem, starred: Boolean?, rating: JsonElement?) =
        ownerOf(account).submit(item, starred, rating)

    override fun pending(account: String, contentId: String): Flow<PendingUserData?> =
        bound.flatMapLatest { it?.takeIf { b -> b.account == account }?.owner?.store?.watch(contentId) ?: flowOf(null) }

    override suspend fun landedSeq(account: String): Long = ownerOf(account).store.landedSeq()

    /** The open owner's rows with a wish; 0 without one. */
    val unsent: Flow<Int> = bound.flatMapLatest { it?.owner?.unsent ?: flowOf(0) }

    /** [unsent] with null until an owner is bound and has read its rows. */
    val unsentOrNull: Flow<AccountRead<Int>?> = bound.flatMapLatest { b ->
        val owner = b?.owner ?: return@flatMapLatest flowOf(null)
        owner.unsent.map { AccountRead(b.account, it) }.onStart<AccountRead<Int>?> { emit(null) }
    }

    /** [stuck] with null until [account]'s owner is bound and has read its rows. */
    fun stuckOrNull(account: String): Flow<List<AttentionItem.Unsynced>?> = bound.flatMapLatest { b ->
        val owner = b?.takeIf { it.account == account }?.owner ?: return@flatMapLatest flowOf(null)
        owner.stuck.map { rows -> rows.map { AttentionItem.Unsynced(it.contentId, it.title, it.lastError) } }.onStart<List<AttentionItem.Unsynced>?> { emit(null) }
    }

    fun stuck(account: String): Flow<List<AttentionItem.Unsynced>> = bound.flatMapLatest { b ->
        val owner = b?.takeIf { it.account == account }?.owner ?: return@flatMapLatest flowOf(emptyList())
        owner.stuck.map { rows -> rows.map { AttentionItem.Unsynced(it.contentId, it.title, it.lastError) } }
    }

    suspend fun retry(account: String, contentId: String) = ownerOf(account).retry(contentId)

    suspend fun discard(account: String, contentId: String) = ownerOf(account).discard(contentId)

    /** A foreground run for [account]'s rows, waiting for its end. */
    suspend fun drain(account: String): DrainResult = ownerOf(account).request(RunKind.FOREGROUND).await()

    /**
     * [SyncWorker]'s drain, for the account whose directory is [accountDir]: once its owner exists, and only
     * while that account is signed in. Null when it isn't, or the owner stops meanwhile. Without a row to
     * send ([periodic]: any; otherwise one that isn't stuck) it answers `DONE` without a request.
     */
    suspend fun drainInBackground(accountDir: String, periodic: Boolean): DrainResult? {
        fun ours(account: String?) = account != null && AccountStores.directoryName(account) == accountDir
        if (signedIn() != null && !ours(signedIn())) return null
        val owner = withTimeoutOrNull(START_WAIT) { bound.first { ours(it?.account) } }
            ?.takeIf { it.account == signedIn() }?.owner ?: return null
        return try {
            if (owner.store.wanted().none { periodic || !it.stuck }) return DrainResult.DONE
            owner.request(if (periodic) RunKind.PERIODIC else RunKind.ONE_TIME).await()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // Still the signed-in account's owner: try later. Otherwise it stopped for an account change, which also cancels the work.
            if (bound.value?.owner === owner && ours(signedIn())) DrainResult.WAITING else null
        }
    }

    /** The app came to the front: the rows are tried again. */
    fun foreground() {
        inForeground = true
        bound.value?.owner?.request(RunKind.FOREGROUND)
    }

    /** The app left the front. */
    fun background() {
        inForeground = false
    }

    private companion object {
        /** How long the worker waits for its account's owner at process start. */
        const val START_WAIT = 10_000L
    }
}
