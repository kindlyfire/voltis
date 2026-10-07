package me.tijlvdb.voltis.data.reader

import android.util.Log
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.merge
import kotlinx.coroutines.job
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.AccountChangedException
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.FileData
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.content.DownloadedCatalog
import me.tijlvdb.voltis.data.downloads.CopyPin
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.data.downloads.DownloadStore
import me.tijlvdb.voltis.data.downloads.manifestIn
import me.tijlvdb.voltis.data.net.isContentMissing
import me.tijlvdb.voltis.data.net.isUnreachable
import me.tijlvdb.voltis.domain.comic.OpenedComic
import me.tijlvdb.voltis.domain.comic.ReaderData
import me.tijlvdb.voltis.domain.downloads.Stale
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.domain.downloads.filesIntact
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.reading.ReadingSync
import me.tijlvdb.voltis.domain.reading.SyncUnavailable

/**
 * The reader's data (P2 §9): a downloaded item from its copy and the cached rows, anything else
 * from the server; the series and its volumes from the cached rows when the server can't be reached.
 *
 * An open is for the account whose downloads are open at that moment, and never falls back once
 * that account is gone. A local open pins its copy for the owner's lifetime, so the copy's
 * directory stays while it is read, and a replaced or deleted copy reopens the reader ([OpenedComic.replaced]).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class OfflineFirstReaderData internal constructor(
    private val network: AccountReaderData,
    private val downloads: StateFlow<DownloadStore?>,
    /** The account whose offline data couldn't be opened. */
    private val failed: StateFlow<String?>,
    private val connectivity: Connectivity,
    private val reading: ReadingSync,
    /** The signed-in account, for when no downloads are open. */
    private val signedIn: () -> String?,
    private val clock: () -> Long = System::currentTimeMillis,
) : ReaderData {
    private val catalog = DownloadedCatalog { downloads.value?.accountStore?.db }

    @Inject
    constructor(network: NetworkReaderData, downloads: DownloadRepository, connectivity: Connectivity, reading: ReadingSync, session: SessionStore) :
        this(network, downloads.current, downloads.failed, connectivity, reading, { session.active()?.account })

    /**
     * A copy, when it is current or the server can't give a newer one: not stale, gone from the
     * server, or updated there while offline (or the open from the server fails). Else the network.
     */
    override suspend fun open(contentId: String, owner: CoroutineScope): OpenedComic {
        val signed = signedIn()
        val store = downloads.value ?: signed?.let { started(it) }
        val account = store?.account ?: signed ?: throw SyncUnavailable.AccountChanged()
        if (store == null) return remote(contentId, account)
        val pin = try {
            store.openCopy(contentId, owner.coroutineContext.job)
        } catch (e: SyncUnavailable.OfflineData) {
            // The downloads couldn't be opened: the server still can.
            null
        } ?: return remote(contentId, account)
        try {
            // A damaged copy, like an outdated one, is read from the server while online.
            // The files are checked for every copy not already damaged, whatever the version says.
            val damaged = pin.stale == Stale.DAMAGED || checkFiles(store, contentId, pin)
            val replaceable = damaged || pin.stale == Stale.VERSION
            if (replaceable && connectivity.online.value) {
                // What this request sees, as of when it starts.
                val at = clock()
                try {
                    return remote(contentId, account).also { pin.release() }
                } catch (e: Exception) {
                    // The account went: never the copy instead. Any other failure: the copy, which is complete.
                    if (e is CancellationException || e is SyncUnavailable) throw e
                    if (e.isUnreachable()) connectivity.unreachable() else if (e.isContentMissing()) markGone(store, contentId, at)
                }
            }
            return local(store, account, contentId, pin, owner) ?: remote(contentId, account).also { pin.release() }
        } catch (e: Throwable) {
            pin.release()
            throw e
        }
    }

    /**
     * The copy's files against what was published (pages present and not empty, the directory's
     * bytes), under the pin. A failure marks the copy damaged, unless the mark can't be stored (a full
     * disk): the open goes on and checks again next time. True when it failed.
     */
    private suspend fun checkFiles(store: DownloadStore, id: String, pin: CopyPin): Boolean {
        val intact = pin.hold()?.use {
            withContext(Dispatchers.IO) {
                val files = pin.dir.listFiles()?.associate { it.name to it.length() } ?: return@withContext false
                filesIntact(pin.pageCount, pin.bytes, { files["$it"] }, files.values.sum())
            }
        } ?: return false
        if (intact) return false
        markDamaged(store, id, pin.copyId)
        return true
    }

    /** Best effort like [markDamaged]: a refused mark (full disk) never keeps the intact copy from opening. */
    private suspend fun markGone(store: DownloadStore, id: String, at: Long) {
        try {
            store.markGone(id, at)
        } catch (e: StorageFullException) {
            Log.w("OfflineFirstReaderData", "Couldn't mark $id gone: storage is full", e)
        }
    }

    private suspend fun markDamaged(store: DownloadStore, id: String, copyId: String) {
        try {
            store.markDamaged(id, copyId)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // Full disk, or the store is gone: the reader still has the copy's pages, and the next open checks again.
            Log.w("OfflineFirstReaderData", "Couldn't mark $id damaged", e)
        }
    }

    /**
     * At process start the reader may open a moment before [account]'s downloads do: they are
     * waited for. Null when they can't be opened, or don't in time.
     */
    private suspend fun started(account: String): DownloadStore? = withTimeoutOrNull(STORE_WAIT) {
        merge(downloads.filter { it?.account == account }, failed.filter { it == account }.map { null }).first()
    }

    /** From the server, for [account]. */
    private suspend fun remote(id: String, account: String): OpenedComic = try {
        network.open(id, ForAccount(account))
    } catch (e: AccountChangedException) {
        throw SyncUnavailable.AccountChanged()
    }

    override suspend fun content(id: String): Content = fallback({ network.content(id) }) { cached(id) }

    override suspend fun volumes(seriesId: String): List<Content> = fallback({ network.volumes(seriesId) }) { cachedVolumes(seriesId) }

    override suspend fun earlierUnread(seriesId: String): String? = if (connectivity.online.value) network.earlierUnread(seriesId) else null

    override suspend fun readable(contentId: String): Boolean =
        connectivity.online.value || withContext(Dispatchers.IO) { downloads.value?.accountStore?.db?.downloads()?.get(contentId)?.copyId != null }

    override fun unreadable(ids: Set<String>, unreachable: Boolean): Flow<Set<String>> {
        val downloaded = downloads.flatMapLatest { store ->
            store?.accountStore?.db?.downloads()?.observe(ids.toList())?.map { rows -> rows.filter { it.copyId != null }.map { it.contentId }.toSet() }
                ?: flowOf(emptySet())
        }
        return combine(connectivity.online, downloaded) { online, done -> if (online && !unreachable) emptySet() else ids - done }.distinctUntilChanged()
    }

    /** The pinned copy, read while held; null when it can't be read. Throws `AccountChanged` once its store has closed. */
    private suspend fun local(store: DownloadStore, account: String, id: String, pin: CopyPin, owner: CoroutineScope): OpenedComic? {
        val read = pin.hold()?.use {
            withContext(Dispatchers.IO) {
                try {
                    AppJson.decodeFromString(Content.serializer(), store.accountStore.db.content().get(id)!!.json) to manifestIn(pin.dir)!!
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    null
                }
            }
        }
        // Before the read counts, or its failure sends the reader to the network.
        store.ensureOpen()
        val (content, manifest) = read ?: return null
        // The page sizes are the copy's own, from its manifest only.
        val pages = manifest.pages.map { JsonArray(listOf(JsonPrimitive(it.name), JsonPrimitive(it.width), JsonPrimitive(it.height))) }
        val shown = content.copy(fileData = FileData(pages)).withShown(account)
        store.ensureOpen()
        val source = LocalPageSource(pin, manifest) { owner.launch { markDamaged(store, id, pin.copyId) } }
        return OpenedComic(shown, source, store.changes(id, pin.copyId))
    }

    /**
     * The lane's projected reading in place of the cached row's `user_data`: the reader resumes from
     * its own lane. Catalog pages use effective reading instead.
     */
    private suspend fun Content.withShown(account: String): Content {
        val shown = withTimeoutOrNull(1_000) { reading.shown(setOf(id), account).first() }?.get(id) ?: return this
        return copy(userData = (userData ?: UserData()).copy(status = shown.status, progress = shown.progress, lastReadAt = shown.lastReadAt))
    }

    private suspend fun cached(id: String): Content? = catalog.stored(id)

    private suspend fun cachedVolumes(seriesId: String): List<Content>? = catalog.volumes(seriesId)

    /** Offline, what is cached first; online, the server, falling back to the cache when it can't be reached. */
    private suspend fun <T> fallback(remote: suspend () -> T, local: suspend () -> T?): T {
        if (!connectivity.online.value) local()?.let { return it }
        return try {
            remote()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            if (!e.isUnreachable()) throw e
            // Starts a probe; `online` follows once it answers, and the reader's availability with it.
            connectivity.unreachable()
            local() ?: throw e
        }
    }

    private companion object {
        const val STORE_WAIT = 10_000L
    }
}
