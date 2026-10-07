package me.tijlvdb.voltis.data.db

import androidx.room.ColumnInfo
import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey

// The tables of P2 §3. JSON is stored as text, server times as their strings, local times as epoch milliseconds.

@Entity(tableName = "reading_lane", indices = [Index("parent_id")])
data class LaneEntity(
    @PrimaryKey @ColumnInfo(name = "content_id") val contentId: String,
    @ColumnInfo(name = "library_id") val libraryId: String?,
    val uri: String?,
    @ColumnInfo(name = "parent_id") val parentId: String?,
    val type: String?,
    val title: String?,
    @ColumnInfo(name = "page_count") val pageCount: Int?,
    val acked: Boolean,
    val rebase: Boolean,
    val revision: String?,
    val status: String?,
    @ColumnInfo(name = "status_updated_at") val statusUpdatedAt: String?,
    val progress: String,
    @ColumnInfo(name = "progress_updated_at") val progressUpdatedAt: String?,
    @ColumnInfo(name = "last_read_at") val lastReadAt: String?,
    /** The `reading_seq` of the state above. */
    val seq: Long,
    @ColumnInfo(name = "foreign_epoch") val foreignEpoch: Int,
    val series: String?,
    val here: String?,
    val tracking: Boolean,
    val failed: Boolean,
    val failures: Int,
    @ColumnInfo(name = "needs_review") val needsReview: Boolean,
    @ColumnInfo(name = "shown_status") val shownStatus: String?,
    @ColumnInfo(name = "shown_progress") val shownProgress: String,
    @ColumnInfo(name = "shown_last_read_at") val shownLastReadAt: String?,
    @ColumnInfo(name = "touched_at") val touchedAt: Long,
)

@Entity(tableName = "reading_op", indices = [Index("content_id", "id")])
data class OpEntity(
    @PrimaryKey(autoGenerate = true) val id: Long,
    @ColumnInfo(name = "content_id") val contentId: String,
    val kind: String,
    val payload: String,
    val sealed: Boolean,
    /** A JSON array of lane IDs. */
    val after: String?,
    @ColumnInfo(name = "sent_seq") val sentSeq: Long?,
    @ColumnInfo(name = "sent_writer") val sentWriter: String?,
    val epoch: Int?,
    val guard: String?,
    val attempts: Int,
    @ColumnInfo(name = "last_error") val lastError: String?,
    @ColumnInfo(name = "created_at") val createdAt: Long,
)

@Entity(tableName = "sync_notice")
data class NoticeEntity(
    @PrimaryKey(autoGenerate = true) val id: Long,
    @ColumnInfo(name = "content_id") val contentId: String?,
    val title: String,
    val kind: String,
    /** A JSON object: `NoticeDetail`. */
    val detail: String,
    @ColumnInfo(name = "created_at") val createdAt: Long,
)

/** A content row as fetched (P2 §3): every downloaded item, its series, and that series' other volumes. */
@Entity(tableName = "content", indices = [Index("library_id", "uri"), Index("parent_id")])
data class ContentEntity(
    @PrimaryKey val id: String,
    @ColumnInfo(name = "library_id") val libraryId: String?,
    val uri: String?,
    @ColumnInfo(name = "parent_id") val parentId: String?,
    val type: String,
    val title: String,
    /** The index in the series' list for `sort=order&sort_order=asc`; null for a series or a standalone item. */
    val position: Int?,
    val valid: Boolean,
    @ColumnInfo(name = "file_mtime") val fileMtime: String?,
    @ColumnInfo(name = "file_size") val fileSize: Long?,
    @ColumnInfo(name = "cover_version") val coverVersion: String?,
    /** The `Content` as fetched. Its reading state isn't read: that is in [ReadingSnapshotEntity]. */
    val json: String,
    /** True when [json] came from `GET /content/:id`, false for a list row. */
    val detail: Boolean,
    @ColumnInfo(name = "fetched_at") val fetchedAt: Long,
    /** A series' row: its complete list of valid volumes was fetched (it may be empty), so their rows are its membership. Otherwise unknown. */
    @ColumnInfo(name = "volumes_known", defaultValue = "0") val volumesKnown: Boolean = false,
)

/**
 * The greatest-seq complete reading state the server has stated for one content, from any response
 * (P2 §3). Rows are written only by the import primitive (`importReading`) and only move forward;
 * what the phone shows and decides from is this state with the outbox's unsent ops applied.
 */
@Entity(tableName = "reading_snapshot")
data class ReadingSnapshotEntity(
    @PrimaryKey @ColumnInfo(name = "content_id") val contentId: String,
    val seq: Long,
    val revision: String?,
    val status: String?,
    @ColumnInfo(name = "status_updated_at") val statusUpdatedAt: String?,
    val progress: String,
    @ColumnInfo(name = "progress_updated_at") val progressUpdatedAt: String?,
    @ColumnInfo(name = "last_read_at") val lastReadAt: String?,
)

/**
 * One downloaded or queued item (P2 §3, §8). [state] is a `DownloadState`, [stale] a `Stale`,
 * [errorKind] an `ErrorKind`. A row has a transfer (being written into `downloads/<transferId>/`),
 * a copy (complete in `downloads/<copyId>/`, never written again), or both. Only `DownloadStore`
 * writes it.
 */
@Entity(
    tableName = "download",
    indices = [
        Index("series_id"), Index("state", "queued_at"),
        Index(value = ["copy_id"], unique = true), Index(value = ["transfer_id"], unique = true),
    ],
)
data class DownloadEntity(
    @PrimaryKey @ColumnInfo(name = "content_id") val contentId: String,
    /** The parent, or the item itself. */
    @ColumnInfo(name = "series_id") val seriesId: String,
    val state: String,
    val error: String? = null,
    @ColumnInfo(name = "error_kind") val errorKind: String? = null,
    /** What [error] doesn't show: a malformed stream's parser message. */
    @ColumnInfo(name = "error_detail") val errorDetail: String? = null,
    val attempts: Int = 0,
    @ColumnInfo(name = "requested_by") val requestedBy: String,
    @ColumnInfo(name = "queued_at") val queuedAt: Long,
    /** The transfer's directory; null exactly when [state] is done. The facts below are its, from its manifest. */
    @ColumnInfo(name = "transfer_id") val transferId: String? = null,
    val version: String? = null,
    @ColumnInfo(name = "page_count") val pageCount: Int? = null,
    @ColumnInfo(name = "pages_done") val pagesDone: Int = 0,
    @ColumnInfo(name = "bytes_done") val bytesDone: Long = 0,
    @ColumnInfo(name = "file_size") val fileSize: Long? = null,
    @ColumnInfo(name = "file_mtime") val fileMtime: String? = null,
    /** The complete copy readers use, until a transfer replaces it; null before the first. */
    @ColumnInfo(name = "copy_id") val copyId: String? = null,
    @ColumnInfo(name = "copy_version") val copyVersion: String? = null,
    @ColumnInfo(name = "copy_page_count") val copyPageCount: Int? = null,
    @ColumnInfo(name = "copy_bytes") val copyBytes: Long = 0,
    @ColumnInfo(name = "copy_file_size") val copyFileSize: Long? = null,
    @ColumnInfo(name = "copy_file_mtime") val copyFileMtime: String? = null,
    /** `cover.jpg` and `series.jpg` are in the copy. */
    @ColumnInfo(name = "copy_cover") val copyCover: Boolean = false,
    @ColumnInfo(name = "copy_series_cover") val copySeriesCover: Boolean = false,
    @ColumnInfo(name = "completed_at") val completedAt: Long? = null,
    /** The [copyId] a reader found damaged (short or unreadable files); a new copy no longer matches it. */
    @ColumnInfo(name = "damaged_copy") val damagedCopy: String? = null,
    /** Derived from the copy's facts, [damagedCopy] and the observation below. */
    val stale: String? = null,
    /** The newest accepted server observation: when its request started (wall clock), and what it saw. */
    @ColumnInfo(name = "seen_at") val seenAt: Long? = null,
    @ColumnInfo(name = "seen_gone") val seenGone: Boolean = false,
    @ColumnInfo(name = "seen_mtime") val seenMtime: String? = null,
    @ColumnInfo(name = "seen_size") val seenSize: Long? = null,
    /** The user asked for it while the volume was completed: "delete after finishing" leaves it. Sticky; only the row's deletion removes it. */
    @ColumnInfo(defaultValue = "0") val keep: Boolean = false,
)

/** A series' automatic downloads (P2 §17): keep the next [keepNext] volumes (0: off), and delete finished ones. */
@Entity(tableName = "series_policy")
data class SeriesPolicyEntity(
    @PrimaryKey @ColumnInfo(name = "series_id") val seriesId: String,
    @ColumnInfo(name = "keep_next") val keepNext: Int,
    @ColumnInfo(name = "delete_finished") val deleteFinished: Boolean,
    @ColumnInfo(name = "updated_at") val updatedAt: Long,
)

/** A volume the series' policy has wanted once: it is never queued by the policy again. */
@Entity(tableName = "auto_offer", primaryKeys = ["series_id", "content_id"])
data class AutoOfferEntity(
    @ColumnInfo(name = "series_id") val seriesId: String,
    @ColumnInfo(name = "content_id") val contentId: String,
)

/** One of the user's lists as last fetched (P4 §7). */
@Entity(tableName = "custom_list")
data class CustomListEntity(
    @PrimaryKey val id: String,
    /** Its index in the last index answer: the server's newest-first order. */
    val position: Int,
    val name: String,
    val description: String?,
    val visibility: String,
    @ColumnInfo(name = "entry_count") val entryCount: Int,
    @ColumnInfo(name = "updated_at") val updatedAt: String,
    /** The index's `covers` as JSON; only the index request writes it. */
    val covers: String,
    /** The `updated_at` of the detail the entry rows came from; null until they were fetched. */
    @ColumnInfo(name = "entries_updated_at") val entriesUpdatedAt: String?,
    /** When the summary was last written, by the index or a detail. */
    @ColumnInfo(name = "fetched_at") val fetchedAt: Long,
    /** When the entry rows were last fetched; null until then. */
    @ColumnInfo(name = "detail_fetched_at") val detailFetchedAt: Long?,
    /** Counts the writes of this list's detail, each a new cache state even when it shows the same rows; only [storeList] bumps it. */
    val revision: Long = 0,
)

/** An entry of a cached list, with enough of its content to draw it offline. [contentId] is null when the content is gone. */
@Entity(tableName = "custom_list_entry", primaryKeys = ["list_id", "library_id", "uri"], indices = [Index("content_id")])
data class CustomListEntryEntity(
    @ColumnInfo(name = "list_id") val listId: String,
    @ColumnInfo(name = "library_id") val libraryId: String,
    val uri: String,
    @ColumnInfo(name = "entry_id") val entryId: String,
    /** Its index in the detail's `entries`. */
    val position: Int,
    val notes: String?,
    @ColumnInfo(name = "content_id") val contentId: String?,
    /** The `uri` when the content is gone. */
    val title: String,
    val type: String?,
    @ColumnInfo(name = "cover_version") val coverVersion: String?,
)

/**
 * A star and rating change of one item, and the last write of it that landed (P4 §7). [wanted] rows
 * hold a wish to send; a row without one is kept for its landed columns until the owner's next start.
 */
@Entity(tableName = "pending_user_data")
data class PendingUserDataEntity(
    @PrimaryKey @ColumnInfo(name = "content_id") val contentId: String,
    @ColumnInfo(name = "library_id") val libraryId: String?,
    val uri: String?,
    val title: String,
    val wanted: Boolean,
    /** The wanted star; null: not changed. */
    val starred: Boolean?,
    @ColumnInfo(name = "rating_set") val ratingSet: Boolean,
    /** With [ratingSet], null means cleared. */
    val rating: Int?,
    val rev: Long,
    @ColumnInfo(name = "landed_seq") val landedSeq: Long?,
    @ColumnInfo(name = "landed_starred") val landedStarred: Boolean,
    @ColumnInfo(name = "landed_rating") val landedRating: Int?,
    val attempts: Int,
    @ColumnInfo(name = "last_error") val lastError: String?,
    @ColumnInfo(name = "created_at") val createdAt: Long,
)
