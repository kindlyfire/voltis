package me.tijlvdb.voltis.domain.reading

import kotlinx.coroutines.flow.Flow
import kotlinx.serialization.json.JsonObject
import me.tijlvdb.voltis.domain.sync.StoredNotice

/** The engine's persisted lanes and outbox (P2 §3). Only the engine writes them. */
interface ReadingStore {
    /** The outbox in order, with the lanes of its ops and every lane that awaits review. */
    suspend fun load(): Stored

    suspend fun lane(contentId: String): Lane?

    /**
     * One transaction: nothing of it is written when any part fails, an update of an op that doesn't
     * exist included. Returns the IDs given to the ops added, in order. Stored notices are announced
     * only once it committed.
     */
    suspend fun commit(change: Change): List<Long>

    /** From the cached rows and their snapshots, for guards made offline; null when the series' list isn't known whole, or a snapshot of it is missing. */
    suspend fun volumes(seriesId: String): SeriesVolumes?

    /**
     * The import primitive, for states that arrive outside a [commit]: stored when a content has none,
     * replacing the stored one only for a greater seq. Equal or lower seqs are ignored, so the order of
     * imports and their repeats don't matter.
     */
    suspend fun import(snapshots: Collection<ReadingSnapshot>)

    /** The newest server state known of [contentId]; null when none was ever imported. */
    suspend fun snapshot(contentId: String): ReadingState?

    /** [ids]' readings: their snapshots with the outbox's unsent ops applied in order. Unknown ids have no entry. */
    suspend fun effective(ids: Collection<String>): Map<String, EffectiveReading>

    /**
     * Deletes lanes untouched since [before] with nothing unsent and not awaiting review, and the
     * snapshots no content row, download, policy, unsent op or held review needs: lanes don't keep those.
     */
    suspend fun prune(before: Long)

    /** The `shown_*` columns of the lanes among [ids], as they change. */
    fun shown(ids: Set<String>): Flow<Map<String, Shown>>

    /** The number of ops in the outbox, as it changes. */
    fun unsent(): Flow<Int>

    /** Lanes awaiting review, titled with their series when it is known, by series and title (else ID), as they change. */
    fun held(): Flow<List<AttentionItem.Held>>

    /** Deletes a stored notice; [commit] writes them too, so the engine's loop runs both. */
    suspend fun dismissNotice(id: Long)

    /** An item's title, from its lane or its cached row: a notice names a volume's series with it. */
    suspend fun title(contentId: String): String?
}

data class Stored(val ops: List<Op>, val lanes: Map<String, Lane>)

/**
 * [ops] with an [Op.id] of 0 are added, others replaced. [notices] go to `sync_notice`. [snapshots] are
 * imported by the primitive in the same transaction: an answer's states are stored with the retiring of its op.
 */
data class Change(
    val snapshots: List<ReadingSnapshot> = emptyList(),
    val lanes: List<Lane> = emptyList(),
    val deleteLanes: List<String> = emptyList(),
    val ops: List<Op> = emptyList(),
    val deleteOps: List<Long> = emptyList(),
    val notices: List<StoredNotice> = emptyList(),
)

/** One item's or series' reading as the engine keeps it: a `reading_lane` row. */
data class Lane(
    val contentId: String,
    val libraryId: String? = null,
    val uri: String? = null,
    val parentId: String? = null,
    val type: String? = null,
    val title: String? = null,
    val pageCount: Int? = null,
    /** False only between `attach()` making the lane and its first `load()`. */
    val acked: Boolean = true,
    /** The server's state is read and adopted before the lane's next op. */
    val rebase: Boolean = false,
    /** The last acknowledged state. */
    val state: ReadingState = ReadingState(),
    /** How many times a state someone else wrote was adopted. */
    val foreignEpoch: Int = 0,
    val series: SeriesInfo? = null,
    val here: JsonObject? = null,
    val tracking: Boolean = true,
    val failed: Boolean = false,
    val failures: Int = 0,
    val needsReview: Boolean = false,
    /** The projection: the acknowledged state with the unsent ops applied, for display. */
    val shownStatus: String? = state.status,
    val shownProgress: JsonObject = state.progress,
    val shownLastReadAt: String? = state.lastReadAt,
    val touchedAt: Long = 0,
)

/** A `reading_op` row. [id] orders the outbox. */
data class Op(
    val id: Long = 0,
    val contentId: String,
    /** One of [OpKind]. */
    val kind: String,
    /** By kind: the progress; the request without writer fields; `{series_id, action, include_unread, until_id}`; `{snapshot, series}`. */
    val payload: JsonObject,
    val sealed: Boolean = false,
    /** Lane IDs whose reading goes first. */
    val after: List<String> = emptyList(),
    /** Set before the request leaves, cleared when its result is known: non-null means the result is unknown. */
    val sentSeq: Long? = null,
    /** The writer [sentSeq] was sent as: a repeat goes as the same (writer, seq) pair, even after the install's identity changed. */
    val sentWriter: String? = null,
    /** The lane's `foreignEpoch` when a command or undo was made. */
    val epoch: Int? = null,
    /** Series ops: `{series: revision, volumes: {id: revision}}`. */
    val guard: JsonObject? = null,
    val attempts: Int = 0,
    val lastError: String? = null,
    val createdAt: Long = 0,
)

object OpKind {
    const val POSITION = "position"
    const val FINISH = "finish"
    const val COMMAND = "command"
    const val SERIES = "series"
    const val UNDO = "undo"
}
