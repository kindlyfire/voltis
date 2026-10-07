package me.tijlvdb.voltis.data.db

import androidx.room.ColumnInfo
import androidx.room.Dao
import androidx.room.Embedded
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import androidx.room.Relation
import androidx.room.Transaction
import androidx.room.Update
import androidx.room.Upsert
import kotlinx.coroutines.flow.Flow

@Dao
interface LaneDao {
    @Query("SELECT * FROM reading_lane WHERE content_id = :contentId")
    suspend fun get(contentId: String): LaneEntity?

    @Query("SELECT * FROM reading_lane WHERE content_id IN (:contentIds)")
    suspend fun get(contentIds: List<String>): List<LaneEntity>

    @Query("SELECT * FROM reading_lane WHERE content_id IN (SELECT content_id FROM reading_op) OR needs_review")
    suspend fun withOpsOrReview(): List<LaneEntity>

    @Upsert
    suspend fun upsert(lanes: List<LaneEntity>)

    @Query("DELETE FROM reading_lane WHERE content_id IN (:contentIds)")
    suspend fun delete(contentIds: List<String>)

    /** Lanes untouched since [before] with nothing unsent and not awaiting review, nor holding a download: `pruneIdle` spares those an op guards. */
    @Query(
        "SELECT content_id FROM reading_lane WHERE touched_at < :before AND NOT needs_review " +
            "AND content_id NOT IN (SELECT content_id FROM reading_op) AND content_id NOT IN (SELECT content_id FROM download)",
    )
    suspend fun idle(before: Long): List<String>

    @Query(
        "SELECT content_id, shown_status, shown_progress, shown_last_read_at, needs_review, " +
            "EXISTS (SELECT 1 FROM reading_op o WHERE o.content_id = l.content_id) AS unsent, " +
            "(shown_status IS NOT status OR shown_progress != progress OR shown_last_read_at IS NOT last_read_at) AS projected " +
            "FROM reading_lane l WHERE content_id IN (:contentIds)",
    )
    fun shown(contentIds: List<String>): Flow<List<ShownRow>>

    /** With the series' title, from its lane or its cached row. */
    @Query(
        "SELECT l.content_id, COALESCE(l.title, l.content_id) AS title, COALESCE(p.title, c.title) AS series FROM reading_lane l " +
            "LEFT JOIN reading_lane p ON p.content_id = l.parent_id LEFT JOIN content c ON c.id = l.parent_id " +
            "WHERE l.needs_review ORDER BY series COLLATE NOCASE, title COLLATE NOCASE, l.content_id",
    )
    fun held(): Flow<List<HeldRow>>

    /** An item's title, from its lane or its cached row. */
    @Query("SELECT COALESCE((SELECT title FROM reading_lane WHERE content_id = :id), (SELECT title FROM content WHERE id = :id))")
    suspend fun title(id: String): String?
}

data class HeldRow(@ColumnInfo(name = "content_id") val contentId: String, val title: String, val series: String?)

data class ShownRow(
    @ColumnInfo(name = "content_id") val contentId: String,
    @ColumnInfo(name = "shown_status") val shownStatus: String?,
    @ColumnInfo(name = "shown_progress") val shownProgress: String,
    @ColumnInfo(name = "shown_last_read_at") val shownLastReadAt: String?,
    @ColumnInfo(name = "needs_review") val needsReview: Boolean,
    val unsent: Boolean,
    /** The unsent ops show something else than the acknowledged state. */
    val projected: Boolean,
)

data class ContinuableRow(@ColumnInfo(name = "content_id") val contentId: String, val type: String)

/** Reads of the snapshot table, and the one statement that writes it: use `importReading`, not this. */
@Dao
interface SnapshotDao {
    /** Inserts when absent, replaces only for a greater [seq]: a lower one is ignored, an equal one is the same state. */
    @Query(
        "INSERT INTO reading_snapshot (content_id, seq, revision, status, status_updated_at, progress, progress_updated_at, last_read_at) " +
            "VALUES (:contentId, :seq, :revision, :status, :statusUpdatedAt, :progress, :progressUpdatedAt, :lastReadAt) " +
            "ON CONFLICT(content_id) DO UPDATE SET seq = excluded.seq, revision = excluded.revision, status = excluded.status, " +
            "status_updated_at = excluded.status_updated_at, progress = excluded.progress, progress_updated_at = excluded.progress_updated_at, " +
            "last_read_at = excluded.last_read_at WHERE excluded.seq > reading_snapshot.seq",
    )
    suspend fun upsertNewer(
        contentId: String,
        seq: Long,
        revision: String?,
        status: String?,
        statusUpdatedAt: String?,
        progress: String,
        progressUpdatedAt: String?,
        lastReadAt: String?,
    )

    /**
     * The items (not series) Continue could open without the server, by identity: a book, or a comic with a complete copy.
     * A type comes from the cached row, else the lane's. Their reading is `effectiveReading`'s, not read here.
     */
    @Query(
        "SELECT s.content_id AS content_id, COALESCE(c.type, l.type) AS type FROM reading_snapshot s " +
            "LEFT JOIN content c ON c.id = s.content_id LEFT JOIN reading_lane l ON l.content_id = s.content_id " +
            "WHERE COALESCE(c.type, l.type) = 'book' " +
            "OR COALESCE(c.type, l.type) = 'comic' AND s.content_id IN (SELECT content_id FROM download WHERE copy_id IS NOT NULL)",
    )
    suspend fun continuable(): List<ContinuableRow>

    @Query("SELECT * FROM reading_snapshot WHERE content_id = :id")
    suspend fun get(id: String): ReadingSnapshotEntity?

    @Query("SELECT * FROM reading_snapshot WHERE content_id IN (:ids)")
    suspend fun get(ids: List<String>): List<ReadingSnapshotEntity>

    /** Snapshots no content row, download, policy, unsent op's own content or held review needs; `pruneSnapshots` also spares what an op guards. */
    @Query(
        "SELECT content_id FROM reading_snapshot WHERE content_id NOT IN (SELECT id FROM content) AND content_id NOT IN (SELECT content_id FROM download) " +
            "AND content_id NOT IN (SELECT series_id FROM series_policy) AND content_id NOT IN (SELECT content_id FROM reading_op) " +
            "AND content_id NOT IN (SELECT content_id FROM reading_lane WHERE needs_review)",
    )
    suspend fun unneeded(): List<String>

    @Query("DELETE FROM reading_snapshot WHERE content_id IN (:ids)")
    suspend fun delete(ids: List<String>)
}

@Dao
interface OpDao {
    /** Whether a lane of [seriesId], or the series' own, has an op in the outbox. */
    @Query(
        "SELECT EXISTS (SELECT 1 FROM reading_op o JOIN reading_lane l ON l.content_id = o.content_id " +
            "WHERE l.content_id = :seriesId OR l.parent_id = :seriesId)",
    )
    suspend fun unsentIn(seriesId: String): Boolean

    /** The outbox in order. */
    @Query("SELECT * FROM reading_op ORDER BY id")
    suspend fun all(): List<OpEntity>

    @Insert
    suspend fun insert(ops: List<OpEntity>): List<Long>

    /** Returns the number of rows updated. */
    @Update
    suspend fun update(ops: List<OpEntity>): Int

    @Query("DELETE FROM reading_op WHERE id IN (:ids)")
    suspend fun delete(ids: List<Long>)

    @Query("SELECT COUNT(*) FROM reading_op")
    fun count(): Flow<Int>
}

@Dao
interface NoticeDao {
    @Insert
    suspend fun insert(notice: NoticeEntity): Long

    @Query("SELECT * FROM sync_notice ORDER BY created_at DESC, id DESC")
    fun observe(): Flow<List<NoticeEntity>>

    @Query("DELETE FROM sync_notice WHERE id = :id")
    suspend fun delete(id: Long)
}

@Dao
interface ContentDao {
    @Query("SELECT * FROM content WHERE id = :id")
    suspend fun get(id: String): ContentEntity?

    @Query("SELECT * FROM content WHERE id IN (:ids)")
    suspend fun get(ids: List<String>): List<ContentEntity>

    @Upsert
    suspend fun upsert(rows: List<ContentEntity>)

    /** A series' valid volumes in the server's order; a row without a position comes first. */
    @Query("SELECT * FROM content WHERE parent_id = :seriesId AND valid ORDER BY position, id")
    suspend fun volumes(seriesId: String): List<ContentEntity>

    /** Whether [seriesId]'s list of volumes was fetched whole ([ContentEntity.volumesKnown]). */
    @Query("UPDATE content SET volumes_known = :known WHERE id = :seriesId")
    suspend fun setVolumesKnown(seriesId: String, known: Boolean)

    @Query("UPDATE content SET json = :json WHERE id = :id")
    suspend fun setJson(id: String, json: String)

    @Query("SELECT id FROM content WHERE parent_id = :seriesId")
    suspend fun childIds(seriesId: String): List<String>

    /** Rows gone from their series' list but kept: out of `volumes()` and the reader's offline siblings. */
    @Query("UPDATE content SET valid = 0 WHERE id IN (:ids)")
    suspend fun invalidate(ids: List<String>)

    /** Of [ids], the rows nothing needs: no download, and no unsent op of their lane. */
    @Query(
        "DELETE FROM content WHERE id IN (:ids) AND id NOT IN (SELECT content_id FROM download) " +
            "AND id NOT IN (SELECT content_id FROM reading_op)",
    )
    suspend fun deleteUnneeded(ids: List<String>)

    /** Rows of the series [seriesIds] (or standalone items) that nothing owns: no download, no policy, no unsent op in their lanes. */
    @Query(
        "DELETE FROM content WHERE coalesce(parent_id, id) IN (:seriesIds) AND coalesce(parent_id, id) NOT IN (SELECT series_id FROM download) " +
            "AND coalesce(parent_id, id) NOT IN " +
            "(SELECT coalesce(l.parent_id, l.content_id) FROM reading_op o JOIN reading_lane l ON l.content_id = o.content_id) " +
            "AND coalesce(parent_id, id) NOT IN (SELECT series_id FROM series_policy)",
    )
    suspend fun prune(seriesIds: List<String>)

    /** Rows fetched before [before] that nothing owns, as [prune] says; later ones may belong to a queueing in flight. */
    @Query(
        "DELETE FROM content WHERE fetched_at < :before AND coalesce(parent_id, id) NOT IN (:skip) AND coalesce(parent_id, id) NOT IN (SELECT series_id FROM download) " +
            "AND coalesce(parent_id, id) NOT IN " +
            "(SELECT coalesce(l.parent_id, l.content_id) FROM reading_op o JOIN reading_lane l ON l.content_id = o.content_id) " +
            "AND coalesce(parent_id, id) NOT IN (SELECT series_id FROM series_policy)",
    )
    suspend fun pruneOlderThan(before: Long, skip: List<String>)
}

/** What cards and covers need of a row. */
data class DownloadMark(
    @ColumnInfo(name = "content_id") val contentId: String,
    @ColumnInfo(name = "series_id") val seriesId: String,
    val state: String,
    val stale: String?,
    @ColumnInfo(name = "copy_id") val copyId: String?,
    @ColumnInfo(name = "copy_cover") val copyCover: Boolean,
    @ColumnInfo(name = "copy_series_cover") val copySeriesCover: Boolean,
    @ColumnInfo(name = "completed_at") val completedAt: Long?,
)

/** A download with what the Downloads screen shows of it and its series. */
data class DownloadRow(
    @Embedded val download: DownloadEntity,
    val title: String?,
    @ColumnInfo(name = "cover_version") val coverVersion: String?,
    val position: Int?,
    @ColumnInfo(name = "series_title") val seriesTitle: String?,
    @ColumnInfo(name = "series_cover_version") val seriesCoverVersion: String?,
)

/** Reads, and the three writes `DownloadStore` makes in its transactions; nothing else writes these rows. */
@Dao
interface DownloadDao {
    /** Rows that exist are left as they are. */
    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insert(rows: List<DownloadEntity>): List<Long>

    @Update
    suspend fun update(row: DownloadEntity)

    @Query("DELETE FROM download WHERE content_id IN (:ids)")
    suspend fun delete(ids: List<String>)

    @Query("SELECT * FROM download WHERE content_id = :id")
    suspend fun get(id: String): DownloadEntity?

    @Query("SELECT * FROM download WHERE content_id = :id")
    fun observe(id: String): Flow<DownloadEntity?>

    @Query("SELECT * FROM download WHERE content_id IN (:ids)")
    fun observe(ids: List<String>): Flow<List<DownloadEntity>>

    /** The queue's head: oldest first, then in insertion order. */
    @Query("SELECT * FROM download WHERE state = 'queued' ORDER BY queued_at, rowid LIMIT 1")
    suspend fun next(): DownloadEntity?

    @Query("SELECT * FROM download WHERE state = 'running'")
    suspend fun running(): List<DownloadEntity>

    @Query("SELECT * FROM download WHERE state = 'queued'")
    suspend fun queued(): List<DownloadEntity>

    /** Whether a row's copy or transfer is [dirId]. */
    @Query("SELECT EXISTS (SELECT 1 FROM download WHERE copy_id = :dirId OR transfer_id = :dirId)")
    suspend fun references(dirId: String): Boolean

    @Query("SELECT content_id, series_id, state, stale, copy_id, copy_cover, copy_series_cover, completed_at FROM download")
    fun marks(): Flow<List<DownloadMark>>

    @Query("SELECT * FROM download WHERE series_id = :seriesId")
    fun series(seriesId: String): Flow<List<DownloadEntity>>

    @Query("SELECT * FROM download WHERE series_id = :seriesId")
    suspend fun inSeries(seriesId: String): List<DownloadEntity>

    @Query("SELECT * FROM download")
    suspend fun all(): List<DownloadEntity>

    @Query("SELECT COUNT(*) FROM download WHERE state IN ('queued', 'running')")
    fun active(): Flow<Int>

    @Query("SELECT COUNT(*) FROM download WHERE state IN ('queued', 'running')")
    suspend fun activeNow(): Int

    @Query(
        "SELECT d.*, c.title AS title, c.cover_version AS cover_version, c.position AS position, " +
            "s.title AS series_title, s.cover_version AS series_cover_version " +
            "FROM download d LEFT JOIN content c ON c.id = d.content_id " +
            "LEFT JOIN content s ON s.id = d.series_id AND d.series_id != d.content_id ORDER BY d.queued_at, d.rowid",
    )
    fun rows(): Flow<List<DownloadRow>>
}

/** Reads of the automatic downloads (P2 §17); the writes are made only by `DownloadStore`, in its transactions. */
@Dao
interface AutoDao {
    @Query("SELECT * FROM series_policy")
    fun observePolicies(): Flow<List<SeriesPolicyEntity>>

    @Query("SELECT * FROM series_policy")
    suspend fun policies(): List<SeriesPolicyEntity>

    @Query("SELECT * FROM series_policy WHERE series_id = :seriesId")
    suspend fun policy(seriesId: String): SeriesPolicyEntity?

    @Query("SELECT content_id FROM auto_offer WHERE series_id = :seriesId")
    suspend fun offers(seriesId: String): List<String>

    @Upsert
    suspend fun upsertPolicy(row: SeriesPolicyEntity)

    @Query("DELETE FROM series_policy WHERE series_id = :seriesId")
    suspend fun deletePolicy(seriesId: String)

    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insertOffers(rows: List<AutoOfferEntity>)

    @Query("DELETE FROM auto_offer WHERE series_id = :seriesId")
    suspend fun deleteOffers(seriesId: String)
}

@Dao
interface ListDao {
    @Query("SELECT * FROM custom_list ORDER BY position")
    fun observe(): Flow<List<CustomListEntity>>

    @Query("SELECT * FROM custom_list WHERE id = :id")
    fun observe(id: String): Flow<CustomListEntity?>

    /** The list with its entries in one read, so its [CustomListEntity.revision] always belongs to the rows beside it. */
    @Transaction
    @Query("SELECT * FROM custom_list WHERE id = :id")
    fun observeWithEntries(id: String): Flow<ListWithEntries?>

    @Query("SELECT * FROM custom_list_entry WHERE list_id = :listId ORDER BY position")
    fun entries(listId: String): Flow<List<CustomListEntryEntity>>

    @Query("SELECT * FROM custom_list_entry WHERE list_id = :listId ORDER BY position")
    suspend fun entriesOf(listId: String): List<CustomListEntryEntity>

    /** The lists holding an item: by its entry key, or by its content ID after a rename moved the key. */
    @Query("SELECT DISTINCT list_id FROM custom_list_entry WHERE (library_id = :libraryId AND uri = :uri) OR content_id = :contentId")
    fun listsHolding(libraryId: String, uri: String, contentId: String): Flow<List<String>>

    @Query("SELECT * FROM custom_list")
    suspend fun all(): List<CustomListEntity>

    @Query("SELECT * FROM custom_list WHERE id = :id")
    suspend fun get(id: String): CustomListEntity?

    /** [get] and [entriesOf] in one read: the revision is of the rows beside it, whatever refreshes meanwhile. */
    @Transaction
    @Query("SELECT * FROM custom_list WHERE id = :id")
    suspend fun getWithEntries(id: String): ListWithEntries?

    /** The cached entry rows per list, those whose content is gone included. */
    @Query("SELECT list_id, COUNT(*) AS n FROM custom_list_entry GROUP BY list_id")
    suspend fun entryCounts(): List<EntryCount>

    @Upsert
    suspend fun upsert(rows: List<CustomListEntity>)

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun insertEntries(rows: List<CustomListEntryEntity>)

    @Query("DELETE FROM custom_list WHERE id IN (:ids)")
    suspend fun delete(ids: List<String>)

    @Query("DELETE FROM custom_list_entry WHERE list_id IN (:ids)")
    suspend fun deleteEntries(ids: List<String>)
}

data class ListWithEntries(
    @Embedded val list: CustomListEntity,
    @Relation(parentColumn = "id", entityColumn = "list_id") val entries: List<CustomListEntryEntity>,
)

data class EntryCount(@ColumnInfo(name = "list_id") val listId: String, val n: Int)

/** Written only through the account's `PendingOwner` (`RoomPendingStore`). */
@Dao
interface PendingUserDataDao {
    @Query("SELECT * FROM pending_user_data WHERE content_id = :id")
    suspend fun get(id: String): PendingUserDataEntity?

    @Query("SELECT * FROM pending_user_data WHERE content_id = :id")
    fun watch(id: String): Flow<PendingUserDataEntity?>

    @Upsert
    suspend fun upsert(row: PendingUserDataEntity)

    @Query("DELETE FROM pending_user_data WHERE content_id = :id")
    suspend fun delete(id: String)

    /** Rows with a wish, oldest first. */
    @Query("SELECT * FROM pending_user_data WHERE wanted ORDER BY created_at, content_id")
    suspend fun wanted(): List<PendingUserDataEntity>

    @Query("SELECT * FROM pending_user_data WHERE wanted ORDER BY created_at, content_id")
    fun watchWanted(): Flow<List<PendingUserDataEntity>>

    @Query("SELECT COALESCE(MAX(landed_seq), 0) FROM pending_user_data")
    suspend fun landedSeq(): Long

    @Query("DELETE FROM pending_user_data WHERE NOT wanted")
    suspend fun deleteUnwanted()

    @Query("UPDATE pending_user_data SET landed_seq = NULL, landed_starred = 0, landed_rating = NULL")
    suspend fun clearLanded()
}
