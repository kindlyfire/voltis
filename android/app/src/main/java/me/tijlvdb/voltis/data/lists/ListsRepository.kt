package me.tijlvdb.voltis.data.lists

import android.util.Log
import android.os.SystemClock
import java.io.IOException
import java.net.ConnectException
import java.net.UnknownHostException
import javax.inject.Inject
import javax.inject.Singleton
import kotlin.coroutines.AbstractCoroutineContextElement
import kotlin.coroutines.CoroutineContext
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableJob
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlin.math.sign
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.job
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.SerializationException
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.AccountChangedException
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.CoverRef
import me.tijlvdb.voltis.data.api.CustomListSummary
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.data.auth.AppScope
import me.tijlvdb.voltis.data.db.AccountScoped
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.data.db.CustomListEntity
import me.tijlvdb.voltis.data.db.CustomListEntryEntity
import me.tijlvdb.voltis.data.net.VoltisAnswer
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.domain.sync.EntryItem
import me.tijlvdb.voltis.domain.sync.EntryKey
import me.tijlvdb.voltis.ui.UiText
import retrofit2.HttpException

data class ListView(
    val id: String,
    val name: String,
    val description: String?,
    /** One of `ListVisibility`. */
    val visibility: String,
    /** Counts entries whose content is gone, as on the web. */
    val entryCount: Int,
    /** The first of the index's covers that has a version. */
    val cover: CoverRef?,
    val updatedAt: String,
)

/** What the open store holds for a list: [detail] null when none; [removed] when it had it and it went; [open] false with no store. */
data class CachedList(val detail: ListDetailView?, val removed: Boolean, val open: Boolean)

/** [entries] leaves out those whose content is gone, as the web hides them. [revision]: which write of the list's detail the cache shows. */
data class ListDetailView(val list: ListView, val entries: List<EntryView>, val revision: Long)

data class EntryView(
    val entryId: String,
    val libraryId: String,
    val uri: String,
    val contentId: String,
    val title: String,
    val type: String?,
    val coverVersion: String?,
    val notes: String?,
) {
    val key get() = EntryKey(libraryId, uri)

    fun item() = EntryItem(contentId, key, title, type.orEmpty(), coverVersion)
}

/** What a refresh did. Only [Ran] and [Gone] say anything about the server's lists. */
sealed interface RefreshOutcome {
    data object Ran : RefreshOutcome

    /** The server can't be reached; nothing was asked. */
    data object Offline : RefreshOutcome

    /** No account store is open (signed out, not open yet, or failed), or the account changed meanwhile. */
    data object NoStore : RefreshOutcome

    /** The server said the list doesn't exist; its rows are deleted. Only [ListsRepository.refreshList] answers it. */
    data object Gone : RefreshOutcome

    data class Failed(val error: Exception) : RefreshOutcome
}

/** An error response to a list request, its body read once into [answer]. */
class HttpFailure(val code: Int, val answer: VoltisAnswer) : Exception("HTTP $code")

/** The server's message when it gave one. */
fun RefreshOutcome.Failed.text(): UiText = when (val e = error) {
    is HttpFailure -> (e.answer as? VoltisAnswer.Genuine)?.message?.let(UiText::Raw) ?: UiText.Res(R.string.error_http, e.code)
    else -> e.toUiText()
}

/** What a direct write did. Writes never throw, except on cancellation. */
sealed interface Write<out T> {
    /**
     * The server applied it. [reload] is the refresh after it: anything but [RefreshOutcome.Ran] means
     * the cache may lag, and only a refresh (never the write again) brings it in. Null when nothing
     * differed, so nothing was sent and no refresh ran: it says nothing about the cache.
     */
    data class Applied<T>(val value: T, val reload: RefreshOutcome?) : Sure<T>

    /** Not applied, or (for a write that states a wanted value) possibly applied: repeating the action is safe. */
    data class Failed(val text: UiText, val reload: RefreshOutcome? = null) : Sure<Nothing>

    /**
     * Sent; whether it landed is unknown, and repeating the action is not safe. Only create and move
     * answer this: the other writes are [Sure]. [reload] is the refresh run after it, so the cache
     * shows what did land; [RefreshOutcome.NoStore] when the account went before one could run.
     */
    data class Unknown(val text: UiText, val reload: RefreshOutcome) : Write<Nothing>
}

/** A write that is applied or not: it never answers [Write.Unknown]. */
sealed interface Sure<out T> : Write<T>

/** [notAdded]: why the item isn't in the new list; null when added, or with nothing to add. */
data class Created(val id: String, val name: String, val notAdded: UiText?)

/** [missing]: content IDs a reloaded target still lacks (P4 decision 19); null when the reload didn't run. */
data class Added(val count: Int, val missing: Set<String>?)

data class Place(val index: Int, val of: Int)

/**
 * [place]: the entry's place among the shown entries of the reloaded cache, by entry ID; null when the
 * reload didn't run or no longer shows it. [revision]: the [ListDetailView.revision] that place is of
 * (0 without one): a screen that displays an older one hasn't caught up, a newer one has superseded it.
 */
data class Moved(val entryId: String, val place: Place?, val revision: Long = 0)

/** A removal: [entryId] is the entry deleted (null when the list had none); [revision] as [Moved]'s, of the reload after it. */
data class Removal(val entryId: String?, val revision: Long)

/** Why a refresh left the cache as it was, for an error with Retry; null when there is nothing to say. */
fun RefreshOutcome.failureText(): UiText? = when (this) {
    is RefreshOutcome.Failed -> text()
    RefreshOutcome.Offline -> UiText.Res(R.string.error_unreachable)
    else -> null
}

/** Null without the `uri` and library a list entry needs (P4 decision 28). */
fun Content.entryItem(): EntryItem? {
    val library = libraryId ?: return null
    return EntryItem(id, EntryKey(library, uri ?: return null), title, type, coverVersion)
}

/**
 * The user's lists, cached in the open account's database and read from it, so they open offline
 * (P4 §9). A refresh only touches the account whose store it started with: its requests are tagged
 * with that account, it runs in that store's generation job, which an account change cancels and
 * joins before the database closes, and it checks the store is still the open one before each write.
 */
@Singleton
class ListsRepository internal constructor(
    private val api: VoltisApi,
    private val current: StateFlow<AccountStore?>,
    /** The signed-in account whose store couldn't be opened, else null. */
    val storeFailed: StateFlow<String?>,
    private val connectivity: Connectivity,
    private val scope: CoroutineScope,
    private val now: () -> Long = System::currentTimeMillis,
    private val elapsed: () -> Long = SystemClock::elapsedRealtime,
) : AccountScoped {
    @Inject
    constructor(api: VoltisApi, stores: AccountStores, connectivity: Connectivity, @AppScope scope: CoroutineScope) :
        this(api, stores.current, stores.failed, connectivity, scope) {
        stores.register(this)
    }

    /** One per open store: its refreshes, its direct writes and its foreground throttle. */
    private class Generation(val store: AccountStore, val job: CompletableJob) {
        var foregroundAt: Long? = null
        var foregroundRunning = false

        /**
         * Held by each direct write for its whole fetch, write and reload, so no two interleave. Lock
         * order: this, then the refresh [mutex]; nothing that holds the mutex takes this (P4 §8).
         */
        val directWrites = Mutex()
    }

    private val lock = Any()

    /** Guarded by [lock]. */
    private var generation: Generation? = null

    // Task phase 7 replaces this with PendingSync's mutex.
    private val mutex = Mutex()

    /**
     * What a direct write knows about itself, in its coroutine context: the [store] it was admitted to,
     * which every refresh inside it is pinned to, and whether a one-shot request was [sent] (set just
     * before it goes out), so a teardown after that isn't mistaken for a refusal.
     */
    private class Attempt(val store: AccountStore) : AbstractCoroutineContextElement(Key) {
        @Volatile var sent = false

        companion object Key : CoroutineContext.Key<Attempt>
    }

    /** A refresh started for a store that is no longer the open one. */
    private class StaleStore : Exception()

    /** A request that isn't safe to repeat may have landed. */
    private class MaybeSent : Exception()

    /** Null while no store is open (opening, failed, or signed out). */
    @OptIn(ExperimentalCoroutinesApi::class)
    val lists: Flow<List<ListView>?> = current.flatMapLatest { store ->
        store?.db?.lists()?.observe()?.map { rows -> rows.map { it.toView() } } ?: flowOf(null)
    }

    /** The list as the open store has it. Removal is only inferred inside one open store (across re-subscriptions), never from the store closing. */
    @OptIn(ExperimentalCoroutinesApi::class)
    fun list(id: String): Flow<CachedList> = current.flatMapLatest { store ->
        val dao = store?.db?.lists() ?: return@flatMapLatest flowOf(CachedList(null, removed = false, open = false))
        dao.observeWithEntries(id).map { row ->
            val detail = row?.let {
                ListDetailView(
                    it.list.toView(),
                    it.entries.sortedBy { e -> e.position }.mapNotNull { e ->
                        e.contentId?.let { contentId -> EntryView(e.entryId, e.libraryId, e.uri, contentId, e.title, e.type, e.coverVersion, e.notes) }
                    },
                    it.list.revision,
                )
            }
            CachedList(detail, removed = seen(store, id, present = detail != null) && detail == null, open = true)
        }
    }

    /** The IDs of the cached lists holding [item]. TODO(P4 task phase 8): apply pending rows. */
    @OptIn(ExperimentalCoroutinesApi::class)
    fun listsHolding(item: EntryItem): Flow<Set<String>> = current.flatMapLatest { store ->
        store?.db?.lists()?.listsHolding(item.key.libraryId, item.key.uri, item.contentId)?.map { it.toSet() } ?: flowOf(emptySet())
    }

    /**
     * The index, then the detail of each list that may have changed: its `updated_at` moved, or its
     * count or covers differ from the cache (a scan changes entries without moving `updated_at`), or
     * its entries are more than a day old.
     */
    suspend fun refresh(): RefreshOutcome = guarded { store ->
        val index = call { api.customLists(ForAccount(store.account)) }
        val time = now()
        checkCurrent(store)
        listGone(store, store.db.storeIndex(index, time))
        val fetched = store.db.lists().all().associateBy { it.id }
        for (list in index) {
            val row = fetched[list.id]
            val stale = row == null || row.entriesUpdatedAt != list.updatedAt || row.detailFetchedAt.let { it == null || time - it > DETAIL_MAX_AGE_MS }
            if (stale) fetch(store, list.id)
        }
        RefreshOutcome.Ran
    }

    /** One list's detail, unconditionally. A list the server no longer has is deleted: [RefreshOutcome.Gone]. */
    suspend fun refreshList(id: String): RefreshOutcome = guarded { store -> if (fetch(store, id)) RefreshOutcome.Ran else RefreshOutcome.Gone }

    // Direct writes (P4 §9, decision 14): online only, nothing queued, one at a time (directWrites).
    // Each answers a [Write]. Create and the reorder may land unseen: a lost answer is Unknown, and only
    // a refresh follows it. The other writes state the value they want, so any failure is Failed.

    /** Then one refresh. With [items], they are added to the new list before it; a failed add still creates. */
    suspend fun create(name: String, description: String?, visibility: String, items: List<EntryItem> = emptyList()): Write<Created> {
        val trimmed = name.trim()
        return write(UiText.Res(R.string.list_create_unknown, trimmed)) { store, writer ->
            val list = try {
                unsafe { writer.create(trimmed, description.blankToNull(), visibility) }
            } catch (_: MaybeSent) {
                return@write Write.Unknown(UiText.Res(R.string.list_create_unknown, trimmed), refresh())
            }
            var notAdded = if (items.isEmpty()) {
                null
            } else {
                try {
                    writer.add(listOf(list.id), items.map { it.contentId })
                    null
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    if (unreachable(e)) connectivity.unreachable()
                    e.writeText()
                }
            }
            val reload = refresh()
            if (items.isNotEmpty() && reload == RefreshOutcome.Ran) {
                // The reloaded list decides, both ways: an add whose answer was lost may have landed.
                checkCurrent(store)
                val entries = store.db.lists().entriesOf(list.id)
                notAdded = if (items.any { entries.find(it) == null }) notAdded ?: UiText.Res(R.string.msg_update_failed) else null
            }
            Write.Applied(Created(list.id, list.name, notAdded), reload)
        }
    }

    /** False when nothing differs from the cached list, and nothing was sent. */
    suspend fun update(id: String, name: String, description: String?, visibility: String): Sure<Boolean> = sure { store, writer ->
        val trimmed = name.trim()
        val text = description.blankToNull()
        val row = store.db.lists().get(id)
        if (row != null && row.name == trimmed && row.description.blankToNull() == text && row.visibility == visibility) {
            return@sure Write.Applied(false, null)
        }
        checkingGone(listOf(id)) { writer.update(id, trimmed, text, visibility) }
        Write.Applied(true, refreshList(id))
    }

    /** A list the server no longer has counts as deleted. Its rows go silently: the user deleted it. */
    suspend fun delete(id: String): Sure<Unit> = sure { store, writer ->
        try {
            writer.delete(id)
        } catch (e: HttpFailure) {
            if ((e.answer as? VoltisAnswer.Genuine)?.code != 404) throw e
        }
        // The server deleted it: a refused cache delete leaves the row for the next refresh, and says why.
        val reload = try {
            mutex.withLock {
                checkCurrent(store)
                listGone(store, listOf(id))
            }
            RefreshOutcome.Ran
        } catch (e: StorageFullException) {
            RefreshOutcome.Failed(e)
        }
        Write.Applied(Unit, reload)
    }

    /**
     * Swaps [item]'s entry with the next entry in [delta]'s direction that has content (the hidden
     * ones stay put), sending every entry's ID in the new order. The list is fetched first, so the
     * order is the server's. Null, with nothing sent, when there is no such neighbour.
     */
    suspend fun move(listId: String, item: EntryItem, delta: Int): Write<Moved?> = write(UiText.Res(R.string.entry_move_unknown)) { store, writer ->
        fetchForWrite(listId)
        val entries = store.db.lists().entriesOf(listId).toMutableList()
        val moved = entries.find(item) ?: throw ListWriteFailure.EntryGone()
        val from = entries.indexOf(moved)
        val step = delta.sign
        var to = from + step
        while (to in entries.indices && entries[to].contentId == null) to += step
        if (to !in entries.indices) return@write Write.Applied(null, RefreshOutcome.Ran)
        entries[from] = entries[to]
        entries[to] = moved
        try {
            checkingGone(listOf(listId)) { unsafe { writer.reorder(listId, entries.map { it.entryId }) } }
        } catch (_: MaybeSent) {
            // Reloaded before the lock is released, so a next move acts on the order that landed.
            return@write Write.Unknown(UiText.Res(R.string.entry_move_unknown), refreshList(listId))
        } catch (e: HttpFailure) {
            // Refused: the list changed under the move (an entry went meanwhile). The reload shows it.
            if ((e.answer as? VoltisAnswer.Genuine)?.code != 400) throw e
            return@write Write.Failed(e.writeText(), refreshList(listId))
        }
        val reload = refreshList(listId)
        var revision = 0L
        val place = if (reload == RefreshOutcome.Ran) {
            checkCurrent(store)
            val stored = store.db.lists().getWithEntries(listId)
            revision = stored?.list?.revision ?: 0
            val shown = stored?.entries.orEmpty().sortedBy { it.position }.filter { it.contentId != null }
            shown.indexOfFirst { it.entryId == moved.entryId }.takeIf { it >= 0 }?.let { Place(it, shown.size) }
        } else {
            null
        }
        Write.Applied(Moved(moved.entryId, place, revision), reload)
    }

    /** The server's count of entries added; then the index, and which items a reloaded list still lacks. */
    suspend fun addEntries(listIds: List<String>, items: List<EntryItem>): Sure<Added> = sure { store, writer ->
        val count = checkingGone(listIds) { writer.add(listIds, items.map { it.contentId }) }
        val reload = refresh()
        val missing = if (reload == RefreshOutcome.Ran) {
            checkCurrent(store)
            val lists = listIds.map { store.db.lists().entriesOf(it) }
            items.filter { item -> lists.any { it.find(item) == null } }.mapTo(HashSet()) { it.contentId }
        } else {
            null
        }
        Write.Applied(Added(count, missing), reload)
    }

    /** The entry ID deleted, or found already gone; null when the list had no entry for [item], and nothing was sent. */
    suspend fun removeEntry(listId: String, item: EntryItem): Sure<Removal> = sure { store, writer ->
        var entryId: String? = null
        val sent = onEntry(store, listId, item) {
            entryId = it.entryId
            writer.remove(listId, it.entryId)
        }
        val reload = if (sent == null) RefreshOutcome.Ran else refreshList(listId)
        var revision = 0L
        if (reload == RefreshOutcome.Ran) {
            checkCurrent(store)
            revision = store.db.lists().get(listId)?.revision ?: 0
        }
        Write.Applied(Removal(entryId, revision), reload)
    }

    /** Blank [notes] clear them; notes equal to the cached ones send nothing. */
    suspend fun setNotes(listId: String, item: EntryItem, notes: String?): Sure<Unit> = sure { store, writer ->
        val text = notes.blankToNull()
        if (entryFor(store, listId, item)?.notes.blankToNull() == text) return@sure Write.Applied(Unit, null)
        onEntry(store, listId, item) { writer.setNotes(listId, it.entryId, text) } ?: throw ListWriteFailure.EntryGone()
        Write.Applied(Unit, refreshList(listId))
    }

    /** The app came to the front: [refresh] at most every 15 minutes per open store, counting only refreshes that ran. */
    fun foreground() {
        scope.launch {
            // At a cold start the account's store opens just after this.
            val store = withTimeoutOrNull(STORE_WAIT_MS) { current.filterNotNull().first() } ?: return@launch
            val claimed = synchronized(lock) {
                val gen = generationOf(store) ?: return@launch
                val last = gen.foregroundAt
                if (gen.foregroundRunning || (last != null && elapsed() - last < FOREGROUND_MS)) return@launch
                gen.foregroundRunning = true
                gen
            }
            var ran = false
            try {
                ran = refresh() == RefreshOutcome.Ran
            } finally {
                // One section, so no other foreground() sees the slot free before its time is set.
                synchronized(lock) {
                    claimed.foregroundRunning = false
                    if (ran) claimed.foregroundAt = elapsed()
                }
            }
        }
    }

    /** The cached entry for [item], fetching the list when the cache has none. */
    private suspend fun entryFor(store: AccountStore, listId: String, item: EntryItem): CustomListEntryEntity? =
        store.db.lists().entriesOf(listId).find(item) ?: run {
            fetchForWrite(listId)
            store.db.lists().entriesOf(listId).find(item)
        }

    /**
     * [write] to [item]'s entry; null when the list has none. When the server says "Entry not found"
     * the list is fetched and, if the entry is there under another ID (removed and added again
     * elsewhere), [write] is retried once on it; if it is gone, null.
     */
    private suspend fun <T> onEntry(store: AccountStore, listId: String, item: EntryItem, write: suspend (CustomListEntryEntity) -> T): T? {
        val first = entryFor(store, listId, item) ?: return null
        try {
            return checkingGone(listOf(listId)) { write(first) }
        } catch (e: HttpFailure) {
            if (e.answer != VoltisAnswer.Genuine(404, ENTRY_NOT_FOUND)) throw e
            fetchForWrite(listId)
            val again = store.db.lists().entriesOf(listId).find(item) ?: return null
            if (again.entryId == first.entryId) throw e
            return checkingGone(listOf(listId)) { write(again) }
        }
    }

    /** [refreshList] for a write that needs the list as the server has it. */
    private suspend fun fetchForWrite(listId: String) {
        when (val outcome = refreshList(listId)) {
            RefreshOutcome.Ran -> Unit
            RefreshOutcome.Gone -> throw ListWriteFailure.ListGone()
            is RefreshOutcome.Failed -> throw outcome.error
            RefreshOutcome.Offline, RefreshOutcome.NoStore -> throw ListWriteFailure.Unavailable()
        }
    }

    /**
     * [block], a write to [listIds]. When it is refused as if a list were gone (404 "List not found";
     * the bulk add answers 403 "Not allowed" for a deleted list, P4 §1), each list is fetched again,
     * which deletes one the server no longer has: that is [ListWriteFailure.ListGone].
     */
    private suspend fun <T> checkingGone(listIds: List<String>, block: suspend () -> T): T = try {
        block()
    } catch (e: HttpFailure) {
        if (e.answer != VoltisAnswer.Genuine(404, LIST_NOT_FOUND) && e.answer != VoltisAnswer.Genuine(403, NOT_ALLOWED)) throw e
        if (listIds.map { refreshList(it) }.contains(RefreshOutcome.Gone)) throw ListWriteFailure.ListGone()
        throw e
    }

    /** [write] for a write that is never unknown: a lost answer is [Write.Failed]. */
    private suspend fun <T> sure(block: suspend (AccountStore, InlineListWriter) -> Write<T>): Sure<T> {
        val outcome = write(null, block)
        // Without an [unknown] text, nothing answers Unknown.
        @Suppress("UNCHECKED_CAST")
        return outcome as Sure<T>
    }

    /**
     * [block] for the open store with its writer, in that store's generation (an account change cancels
     * and joins it) and holding its [Generation.directWrites]. Offline, after a probe that fails too,
     * nothing is sent. What [block] throws is [Write.Failed]. A write that sends a request that isn't
     * safe to repeat ([unsafe]) gives [unknown], not Failed, when the account goes after the send.
     */
    private suspend fun <T> write(unknown: UiText?, block: suspend (AccountStore, InlineListWriter) -> Write<T>): Write<T> {
        val unavailable = Write.Failed(UiText.Res(R.string.msg_update_failed))
        val store = current.value ?: return unavailable
        if (!connectivity.online.value && !connectivity.probe()) return unavailable
        val attempt = Attempt(store)
        // Refused before sending, or torn down after it.
        val gone = { if (unknown != null && attempt.sent) Write.Unknown(unknown, RefreshOutcome.NoStore) else unavailable }
        val run = admit(store) { gen ->
            gen.directWrites.withLock { withContext(attempt) { block(store, InlineListWriter(api, store.account)) } }
        } ?: return unavailable
        return try {
            run.await()
        } catch (e: CancellationException) {
            currentCoroutineContext().ensureActive()
            gone()
        } catch (_: StaleStore) {
            gone()
        } catch (_: AccountChangedException) {
            unavailable
        } catch (e: Exception) {
            if (unreachable(e)) connectivity.unreachable()
            Write.Failed(e.writeText(), if (e is ListWriteFailure.ListGone) RefreshOutcome.Gone else null)
        }
    }

    /** [block], a request that isn't safe to repeat: a failure after which it may have landed is [MaybeSent]. */
    private suspend fun <T> unsafe(block: suspend () -> T): T = try {
        currentCoroutineContext()[Attempt]?.sent = true
        block()
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        val landed = when (e) {
            // Refused before anything was sent: these requests are SendOnce, so no earlier try was sent either.
            is ConnectException, is UnknownHostException, is AccountChangedException -> false
            // A 5xx can follow a commit; an answer that isn't Voltis' says nothing.
            is HttpFailure -> e.answer == VoltisAnswer.Unreachable || e.code >= 500
            // A lost answer (also one relabelled LocalNetworkException), one that can't be read, or anything unforeseen.
            else -> true
        }
        if (!landed) {
            currentCoroutineContext()[Attempt]?.sent = false
            throw e
        }
        if (unreachable(e)) connectivity.unreachable()
        throw MaybeSent()
    }

    /** [block] in [store]'s generation; null when [store] is no longer the open one. Admission and teardown are atomic. */
    private fun <T> admit(store: AccountStore, block: suspend (Generation) -> T): Deferred<T>? = synchronized(lock) {
        val gen = generationOf(store) ?: return null
        scope.async(gen.job) { block(gen) }
    }

    /** Lists [seenStore] has shown, kept across re-subscriptions so a list that goes from it reads as removed. Under [lock]. */
    private var seenStore: AccountStore? = null
    private val seenIds = HashSet<String>()

    /** Records [id] as shown in [store] when [present]; whether [store] has shown it. Another store starts over. */
    private fun seen(store: AccountStore, id: String, present: Boolean): Boolean = synchronized(lock) {
        if (seenStore !== store) {
            seenStore = store
            seenIds.clear()
        }
        if (present) seenIds += id
        id in seenIds
    }

    override suspend fun stop() {
        val old = synchronized(lock) {
            seenStore = null
            seenIds.clear()
            generation.also { generation = null }
        }
        old?.job?.cancelAndJoin()
    }

    /** [store]'s generation, made on first use; null when [store] is no longer the open one. Under [lock]. */
    private fun generationOf(store: AccountStore): Generation? {
        if (current.value !== store) return null
        generation?.let { if (it.store === store) return it }
        return Generation(store, SupervisorJob(scope.coroutineContext.job)).also { generation = it }
    }

    private fun checkCurrent(store: AccountStore) {
        if (current.value !== store) throw StaleStore()
    }

    // Task phase 8 replaces this body with PendingSync.listGone.
    private suspend fun listGone(store: AccountStore, ids: List<String>) {
        if (ids.isEmpty()) return
        checkCurrent(store)
        store.db.deleteLists(ids)
    }

    /** False when the server said the list doesn't exist, and its rows were deleted. */
    private suspend fun fetch(store: AccountStore, id: String): Boolean {
        val detail = try {
            call { api.customList(id, ForAccount(store.account)) }
        } catch (e: HttpFailure) {
            if (e.answer == VoltisAnswer.Genuine(404, LIST_NOT_FOUND)) {
                try {
                    listGone(store, listOf(id))
                } catch (e: StorageFullException) {
                    // The answer stands: the server doesn't have it. The row goes with a later refresh.
                    Log.w("ListsRepository", "Couldn't delete a gone list's rows: storage is full", e)
                }
                return false
            }
            throw e
        }
        checkCurrent(store)
        store.db.storeList(detail, now())
        return true
    }

    /**
     * [block] for the open store (waiting briefly for one to open), in that store's generation and one
     * at a time. Offline, after a probe that fails too, nothing happens.
     */
    private suspend fun guarded(block: suspend (AccountStore) -> RefreshOutcome): RefreshOutcome {
        // A write's refresh stays with the store the write was admitted to.
        val store = currentCoroutineContext()[Attempt]?.store
            ?: current.value
            ?: (if (storeFailed.value == null) withTimeoutOrNull(STORE_WAIT_MS) { current.filterNotNull().first() } else null)
            ?: return RefreshOutcome.NoStore
        if (!connectivity.online.value && !connectivity.probe()) return RefreshOutcome.Offline
        // A refresh is either in the generation stop() joins, or not started.
        val run = admit(store) {
            mutex.withLock {
                checkCurrent(store)
                block(store)
            }
        } ?: return RefreshOutcome.NoStore
        return try {
            run.await()
        } catch (e: CancellationException) {
            // The generation was torn down; a cancelled caller rethrows.
            currentCoroutineContext().ensureActive()
            RefreshOutcome.NoStore
        } catch (_: StaleStore) {
            RefreshOutcome.NoStore
        } catch (_: AccountChangedException) {
            // Refused before it was sent: the account changed, the server is fine.
            RefreshOutcome.NoStore
        } catch (e: Exception) {
            if (unreachable(e)) connectivity.unreachable()
            RefreshOutcome.Failed(e)
        }
    }

    /** No answer, or one that isn't Voltis' (a gateway's 502, a portal's HTML 200). */
    private fun unreachable(e: Exception) = when (e) {
        is IOException, is SerializationException -> true
        is HttpFailure -> e.answer == VoltisAnswer.Unreachable
        else -> false
    }

    private companion object {
        const val FOREGROUND_MS = 15 * 60_000L
        const val STORE_WAIT_MS = 10_000L
        const val DETAIL_MAX_AGE_MS = 24 * 60 * 60_000L
        const val LIST_NOT_FOUND = "List not found"
        const val ENTRY_NOT_FOUND = "Entry not found"
        const val NOT_ALLOWED = "Not allowed"
    }
}

private fun CustomListEntity.toView() =
    ListView(id, name, description, visibility, entryCount, coverRefs().firstOrNull { it.coverVersion != null }, updatedAt)

/** [item]'s entry: by key, else by content ID (P4 decision 18). */
private fun List<CustomListEntryEntity>.find(item: EntryItem) =
    firstOrNull { it.libraryId == item.key.libraryId && it.uri == item.key.uri } ?: firstOrNull { it.contentId == item.contentId }

private fun String?.blankToNull() = takeUnless { it.isNullOrBlank() }

/** [block] with an error response read once, as [HttpFailure]. */
internal suspend fun <T> call(block: suspend () -> T): T = try {
    block()
} catch (e: HttpException) {
    throw HttpFailure(e.code(), VoltisAnswer.of(e.code(), e.response()?.errorBody()?.string()))
}

/** Why a direct write didn't happen, beside an [HttpFailure] with the server's answer. */
private sealed class ListWriteFailure : Exception() {
    /** The server no longer has the list; its rows are deleted. */
    class ListGone : ListWriteFailure()

    /** The list no longer has the entry. */
    class EntryGone : ListWriteFailure()

    /** The server can't be reached or the store went: the list can't be fetched first. */
    class Unavailable : ListWriteFailure()
}

/** A direct write's failure: the server's message, or "Couldn't update". */
private fun Throwable.writeText(): UiText = when (this) {
    is ListWriteFailure.ListGone -> UiText.Res(R.string.list_gone)
    is ListWriteFailure.EntryGone -> UiText.Res(R.string.entry_gone)
    is HttpFailure -> (answer as? VoltisAnswer.Genuine)?.message?.let(UiText::Raw) ?: UiText.Res(R.string.msg_update_failed)
    else -> UiText.Res(R.string.msg_update_failed)
}
