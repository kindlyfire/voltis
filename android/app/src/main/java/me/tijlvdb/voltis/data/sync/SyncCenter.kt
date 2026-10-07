package me.tijlvdb.voltis.data.sync

import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import me.tijlvdb.voltis.data.reading.ReadingSyncManager
import me.tijlvdb.voltis.domain.reading.AttentionItem
import me.tijlvdb.voltis.domain.reading.DrainResult
import me.tijlvdb.voltis.domain.reading.itemCount

/** A value read from the sync owners of [account]. */
class AccountRead<T>(val account: String, val value: T)

/**
 * The one entry point for draining and for the unsent count, over the reading engine and the pending rows
 * (P4 decision 7). The two drains run concurrently and independently: the pending one runs even while the
 * reading one answers `BUSY`.
 */
@Singleton
class SyncCenter @Inject constructor(private val reading: ReadingSyncManager, private val pending: PendingSync) {
    /** [unsent], null until both sources of one account have read their rows (a zero before that would be a guess), and again when either owner changes. */
    val unsentOrNull: Flow<AccountRead<Int>?> = combine(reading.unsentOrNull, pending.unsentOrNull) { a, b ->
        if (a == null || b == null || a.account != b.account) null else AccountRead(a.account, a.value + b.value)
    }

    /** Ops in the reading outbox plus pending stars and ratings. */
    val unsent: Flow<Int> = combine(reading.unsent, pending.unsent) { a, b -> a + b }

    /** Held lanes, stored notices and stuck stars and ratings of [account]. */
    fun attention(account: String): Flow<List<AttentionItem>> =
        combine(reading.attention(account), pending.stuck(account)) { a, b -> a + b }

    /** The count of [attention], null until both sources have read their stored rows (a zero before that would be a guess). */
    fun attentionCountOrNull(account: String): Flow<Int?> =
        combine(reading.attentionOrNull(account), pending.stuckOrNull(account)) { a, b -> if (a == null || b == null) null else (a + b).itemCount() }
            .distinctUntilChanged()

    fun attentionCount(account: String): Flow<Int> = attention(account).map { it.itemCount() }.distinctUntilChanged()

    /** `BUSY` if the reading drain is, else `WAITING` if either is. Throws only when both can't run. */
    suspend fun drain(account: String): DrainResult = coroutineScope {
        val a = async { runCatching { reading.drain(account) } }
        val b = async { runCatching { pending.drain(account) } }
        val ra = a.await()
        val rb = b.await()
        // A cancelled caller is cancelled: runCatching caught it, and the next suspension point rethrows.
        if (ra.isFailure && rb.isFailure) throw ra.exceptionOrNull()!!
        combined(listOfNotNull(ra.getOrNull(), rb.getOrNull()))
    }

    /** [SyncWorker]'s: both at once; null only when both are. */
    suspend fun drainInBackground(accountDir: String, periodic: Boolean): DrainResult? = coroutineScope {
        val a = async { reading.drainInBackground(accountDir, onlyIfUnsent = periodic) }
        val b = async { pending.drainInBackground(accountDir, periodic) }
        val results = listOfNotNull(a.await(), b.await())
        if (results.isEmpty()) null else combined(results)
    }

    /** The reading engine first, as it was. */
    fun foreground() {
        reading.foreground()
        pending.foreground()
    }

    fun background() {
        pending.background()
    }

    suspend fun retry(account: String, contentId: String) = pending.retry(account, contentId)

    suspend fun discard(account: String, contentId: String) = pending.discard(account, contentId)

    private fun combined(results: List<DrainResult>) = when {
        DrainResult.BUSY in results -> DrainResult.BUSY
        DrainResult.WAITING in results -> DrainResult.WAITING
        else -> DrainResult.DONE
    }
}
