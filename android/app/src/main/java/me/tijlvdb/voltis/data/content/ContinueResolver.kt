package me.tijlvdb.voltis.data.content

import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withTimeoutOrNull
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ContinueEntry
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.data.net.isUnreachable
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.net.settled

/** What the Continue shortcut opens: a comic in the reader, anything else (a book) on its page. */
sealed interface ContinueOpen {
    data class Reader(val contentId: String) : ContinueOpen
    data class Page(val contentId: String) : ContinueOpen

    /** Nothing to continue without the server: a fresh start lands on Downloads. */
    data object NothingOffline : ContinueOpen
}

/** The Continue shortcut's item (P5 §6): Home's first Continue reading card, else, without the server, the item read last: by its effective reading. */
class ContinueResolver internal constructor(
    private val connectivity: Connectivity,
    private val remote: suspend () -> List<ContinueEntry>,
    /** Null when the local data can't be read, or the account changed meanwhile. */
    private val candidates: suspend () -> List<ContinueCandidate>?,
) {
    @Inject
    constructor(content: ContentRepository, connectivity: Connectivity, stores: AccountStores, session: SessionStore) : this(
        connectivity,
        { content.continueReading(1, TIMEOUT_SECONDS) },
        {
            // On a cold start the account's store opens after the session settles: wait for it, unless it failed.
            val store = stores.current.value
                ?: (if (stores.failed.value == null) withTimeoutOrNull(STORE_WAIT_MS) { stores.current.filterNotNull().first() } else null)
            val rows = store?.takeIf { it.account == session.active()?.account }?.db?.continueCandidates().orEmpty()
            // The store may have closed (or another account's opened) during the query: nothing of it is used.
            rows.takeIf { store == null || stores.current.value === store }
        },
    )

    /**
     * Null when the server has nothing to continue, or gave another failure than no answer. Waits
     * (cancellably) for the start's check first, so a cold offline start makes no request.
     */
    suspend fun resolve(): ContinueOpen? {
        connectivity.settled()
        if (connectivity.online.value) {
            try {
                return remote().firstOrNull()?.item?.let { open(it.type, it.id) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                if (!e.isUnreachable()) return null
                connectivity.unreachable()
            }
        }
        // A damaged or closing database fails here rather than crashing the caller: the current screen stays.
        val rows = try {
            candidates()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            null
        } ?: return null
        return rows.maxByOrNull { it.recency }?.let { open(it.type, it.contentId) } ?: ContinueOpen.NothingOffline
    }

    private fun open(type: String, id: String) = if (type == ContentType.COMIC) ContinueOpen.Reader(id) else ContinueOpen.Page(id)

    private companion object {
        const val TIMEOUT_SECONDS = 3
        const val STORE_WAIT_MS = 3_000L
    }
}
