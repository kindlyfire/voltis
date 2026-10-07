package me.tijlvdb.voltis.domain.sync

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.serialization.Serializable
import me.tijlvdb.voltis.data.db.OpenAccount

/** Notices nobody was there to see, kept for the open account until dismissed. The reading engine and other writers share it. */
interface SyncNotices {
    /**
     * For writers other than the reading engine, whose store writes notices in its own transactions.
     * Written to [store] itself, the one the writer ran under, never to whichever is open now: it fails
     * when [store] is no longer the open one. Without [announce] it isn't in [fresh]: its writer says
     * so itself (a bulk batch's summary).
     */
    suspend fun insert(store: OpenAccount, notice: StoredNotice, announce: Boolean = true)

    /** Newest first. */
    fun observe(): Flow<List<StoredNotice>>

    /** Each notice once, as it is stored: the scaffold's snackbar. */
    val fresh: SharedFlow<StoredNotice>
}

/** [id] is 0 until stored. [kind] is one of [NoticeKind], or a kind a later phase adds. */
data class StoredNotice(
    val id: Long,
    val contentId: String?,
    val title: String,
    val kind: String,
    val detail: NoticeDetail,
    val createdAt: Long,
)

/** This phase writes [command], [uncertain] and [message]; roadmap Phase 4 adds the rest. */
@Serializable
data class NoticeDetail(
    val command: String? = null,
    /** The command's earlier send had an unknown result: it may have been applied. */
    val uncertain: Boolean = false,
    val message: String? = null,
    val list: String? = null,
    val entry: String? = null,
    val note: String? = null,
)

object NoticeKind {
    const val CHANGED_ELSEWHERE = "changed_elsewhere"
    const val REFUSED = "refused"
    const val GONE = "gone"
}
