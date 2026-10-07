package me.tijlvdb.voltis.data.reading

import androidx.room.withTransaction
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.JsonObject
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.db.LaneEntity
import me.tijlvdb.voltis.data.db.OpEntity
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.db.importReading
import me.tijlvdb.voltis.data.db.pruneLanes
import me.tijlvdb.voltis.data.db.pruneSnapshots
import me.tijlvdb.voltis.data.db.toState
import me.tijlvdb.voltis.data.storage.localTransaction
import me.tijlvdb.voltis.data.sync.toEntity
import me.tijlvdb.voltis.domain.reading.AttentionItem
import me.tijlvdb.voltis.domain.reading.Change
import me.tijlvdb.voltis.domain.reading.Lane
import me.tijlvdb.voltis.domain.reading.Op
import me.tijlvdb.voltis.domain.reading.ReadingSnapshot
import me.tijlvdb.voltis.domain.reading.ReadingState
import me.tijlvdb.voltis.domain.reading.ReadingStore
import me.tijlvdb.voltis.domain.reading.SeriesInfo
import me.tijlvdb.voltis.domain.reading.SeriesVolumes
import me.tijlvdb.voltis.domain.reading.Shown
import me.tijlvdb.voltis.domain.reading.Stored
import me.tijlvdb.voltis.domain.reading.VolumeRef
import me.tijlvdb.voltis.domain.reading.titled
import me.tijlvdb.voltis.domain.reading.userData
import me.tijlvdb.voltis.domain.sync.StoredNotice

/**
 * [ReadingStore] over an account's database. [announce] (`RoomSyncNotices.announce`) gets the
 * notices a commit stored, strictly after its transaction committed.
 */
class RoomReadingStore(private val db: VoltisDatabase, private val announce: suspend (List<StoredNotice>) -> Unit) : ReadingStore {
    override suspend fun load() = db.withTransaction {
        Stored(db.ops().all().map { it.toOp() }, db.lanes().withOpsOrReview().associate { it.contentId to it.toLane() })
    }

    override suspend fun lane(contentId: String) = db.lanes().get(contentId)?.toLane()

    override suspend fun commit(change: Change): List<Long> {
        val (ids, notices) = db.localTransaction {
            // With the retiring of an op: an answer's states and the end of its intent are one commit.
            db.importReading(change.snapshots)
            if (change.deleteOps.isNotEmpty()) db.ops().delete(change.deleteOps)
            if (change.deleteLanes.isNotEmpty()) db.lanes().delete(change.deleteLanes)
            if (change.lanes.isNotEmpty()) db.lanes().upsert(change.lanes.map { it.toEntity() })
            val (added, changed) = change.ops.partition { it.id == 0L }
            if (changed.isNotEmpty()) check(db.ops().update(changed.map { it.toEntity() }) == changed.size) { "No such op" }
            val ids = if (added.isEmpty()) emptyList() else db.ops().insert(added.map { it.toEntity() })
            ids to change.notices.map { it.copy(id = db.notices().insert(it.toEntity())) }
        }
        if (notices.isNotEmpty()) announce(notices)
        return ids
    }

    override suspend fun volumes(seriesId: String): SeriesVolumes? = db.withTransaction {
        val series = db.content().get(seriesId)?.takeIf { it.volumesKnown } ?: return@withTransaction null
        val volumes = db.content().volumes(seriesId)
        // A volume cached without its place (queued without the server) leaves the order unknown.
        if (volumes.any { it.position == null }) return@withTransaction null
        val states = db.snapshots().get(listOf(series.id) + volumes.map { it.id }).associate { it.contentId to it.toState() }
        // A volume without a state isn't known untouched: it is unknown.
        val own = states[series.id] ?: return@withTransaction null
        SeriesVolumes(own.revision, volumes.map { VolumeRef(it.id, (states[it.id] ?: return@withTransaction null).userData()) })
    }

    override suspend fun import(snapshots: Collection<ReadingSnapshot>) = db.importReading(snapshots)

    override suspend fun snapshot(contentId: String) = db.snapshots().get(contentId)?.toState()

    override suspend fun effective(ids: Collection<String>) = db.effectiveReading(ids)

    override suspend fun prune(before: Long) = db.localTransaction {
        db.pruneLanes(before)
        db.pruneSnapshots()
    }

    override fun shown(ids: Set<String>): Flow<Map<String, Shown>> = db.lanes().shown(ids.toList()).map { rows ->
        rows.associate { it.contentId to Shown(it.shownStatus, it.shownProgress.toJsonObject(), it.shownLastReadAt, it.unsent, it.needsReview, it.projected) }
    }

    override fun unsent(): Flow<Int> = db.ops().count()

    override fun held(): Flow<List<AttentionItem.Held>> = db.lanes().held().map { rows -> rows.map { AttentionItem.Held(it.contentId, titled(it.series, it.title)) } }

    override suspend fun dismissNotice(id: Long) = db.localTransaction { db.notices().delete(id) }

    override suspend fun title(contentId: String) = db.lanes().title(contentId)
}

private val idList = ListSerializer(String.serializer())

private fun JsonObject.encode() = AppJson.encodeToString(JsonObject.serializer(), this)

internal fun String.toJsonObject() = AppJson.decodeFromString(JsonObject.serializer(), this)

private fun Lane.toEntity() = LaneEntity(
    contentId, libraryId, uri, parentId, type, title, pageCount, acked, rebase,
    state.revision, state.status, state.statusUpdatedAt, state.progress.encode(), state.progressUpdatedAt, state.lastReadAt, state.seq,
    foreignEpoch, series?.let { AppJson.encodeToString(SeriesInfo.serializer(), it) }, here?.encode(),
    tracking, failed, failures, needsReview, shownStatus, shownProgress.encode(), shownLastReadAt, touchedAt,
)

internal fun LaneEntity.toLane() = Lane(
    contentId, libraryId, uri, parentId, type, title, pageCount, acked, rebase,
    ReadingState(revision, status, statusUpdatedAt, progress.toJsonObject(), progressUpdatedAt, lastReadAt, seq),
    foreignEpoch, series?.let { AppJson.decodeFromString(SeriesInfo.serializer(), it) }, here?.toJsonObject(),
    tracking, failed, failures, needsReview, shownStatus, shownProgress.toJsonObject(), shownLastReadAt, touchedAt,
)

internal fun Op.toEntity() = OpEntity(
    id, contentId, kind, payload.encode(), sealed, after.takeIf { it.isNotEmpty() }?.let { AppJson.encodeToString(idList, it) },
    sentSeq, sentWriter, epoch, guard?.encode(), attempts, lastError, createdAt,
)

internal fun OpEntity.toOp() = Op(
    id, contentId, kind, payload.toJsonObject(), sealed, after?.let { AppJson.decodeFromString(idList, it) }.orEmpty(),
    sentSeq, sentWriter, epoch, guard?.toJsonObject(), attempts, lastError, createdAt,
)
