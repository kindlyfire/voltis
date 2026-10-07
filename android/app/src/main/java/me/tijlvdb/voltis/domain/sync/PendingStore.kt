package me.tijlvdb.voltis.domain.sync

import kotlinx.coroutines.flow.Flow
import kotlinx.serialization.json.JsonElement
import me.tijlvdb.voltis.data.api.UserData

/** One account's pending rows. Each [commit] is one transaction (P4 §7). */
interface PendingStore {
    /** Rows with a wish, oldest first. */
    suspend fun wanted(): List<PendingUserData>

    fun watchWanted(): Flow<List<PendingUserData>>

    /** The item's row, wish or landed. */
    fun watch(contentId: String): Flow<PendingUserData?>

    /** `max(landed_seq)`, 0 without one. */
    suspend fun landedSeq(): Long

    /** The owner's start: rows without a wish deleted, the landed columns of the rest cleared. */
    suspend fun prune()

    /** Applies [change] in one transaction and returns the notices it stored, for announcing after the commit. */
    suspend fun commit(change: PendingChange): List<StoredNotice>
}

sealed interface PendingChange {
    val contentId: String

    data class Submit(val item: UserDataItem, val starred: Boolean?, val rating: JsonElement?, val now: Long) : PendingChange {
        override val contentId get() = item.contentId
    }

    data class Landed(override val contentId: String, val sentRev: Long, val answer: UserData) : PendingChange

    /** Counts only at an unchanged [sentRev]. */
    data class Failed(override val contentId: String, val sentRev: Long, val error: String) : PendingChange

    data class Dropped(override val contentId: String, val sentRev: Long, val notice: StoredNotice) : PendingChange

    /** Retry. */
    data class Reset(override val contentId: String) : PendingChange

    data class Discard(override val contentId: String) : PendingChange
}
