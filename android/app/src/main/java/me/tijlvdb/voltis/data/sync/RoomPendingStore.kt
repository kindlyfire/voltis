package me.tijlvdb.voltis.data.sync

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.db.PendingUserDataEntity
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.db.importReading
import me.tijlvdb.voltis.data.downloads.patchUserData
import me.tijlvdb.voltis.data.storage.localTransaction
import me.tijlvdb.voltis.domain.reading.snapshotOf
import me.tijlvdb.voltis.domain.sync.LandedUserData
import me.tijlvdb.voltis.domain.sync.PendingChange
import me.tijlvdb.voltis.domain.sync.PendingStore
import me.tijlvdb.voltis.domain.sync.PendingUserData
import me.tijlvdb.voltis.domain.sync.StoredNotice
import me.tijlvdb.voltis.domain.sync.after
import me.tijlvdb.voltis.domain.sync.drops

/** [PendingStore] over an account's database. Every change is one [localTransaction] (P4 §7). */
class RoomPendingStore(private val db: VoltisDatabase) : PendingStore {
    private val dao get() = db.pendingUserData()

    override suspend fun wanted() = dao.wanted().map { it.toRow() }

    override fun watchWanted(): Flow<List<PendingUserData>> = dao.watchWanted().map { rows -> rows.map { it.toRow() } }

    override fun watch(contentId: String): Flow<PendingUserData?> = dao.watch(contentId).map { it?.toRow() }

    override suspend fun landedSeq() = dao.landedSeq()

    override suspend fun prune() = db.localTransaction {
        dao.deleteUnwanted()
        dao.clearLanded()
    }

    override suspend fun commit(change: PendingChange): List<StoredNotice> = db.localTransaction {
        val row = dao.get(change.contentId)?.toRow()
        var seq = 0L
        if (change is PendingChange.Landed) {
            // The cached row follows the server's answer, with the landing: a stored page shows it.
            val answer = change.answer
            db.patchUserData(change.contentId, answer.starred, answer.rating?.let { JsonPrimitive(it) } ?: JsonNull)
            // The receipt carries the item's whole reading state too: imported with the landing, so a change made elsewhere is known now.
            db.importReading(listOf(snapshotOf(change.contentId, answer)))
            seq = dao.landedSeq() + 1
        }
        val after = row.after(change, seq)
        when {
            after == null -> dao.delete(change.contentId)
            after != row -> dao.upsert(after.toEntity())
        }
        if (change is PendingChange.Dropped && row.drops(change)) listOf(change.notice.copy(id = db.notices().insert(change.notice.toEntity()))) else emptyList()
    }
}

private fun PendingUserDataEntity.toRow() = PendingUserData(
    contentId, libraryId, uri, title, wanted, starred, ratingSet, rating, rev, attempts, lastError, createdAt,
    landedSeq?.let { LandedUserData(it, landedStarred, landedRating) },
)

private fun PendingUserData.toEntity() = PendingUserDataEntity(
    contentId, libraryId, uri, title, wanted, starred, ratingSet, rating, rev,
    landed?.seq, landed?.starred ?: false, landed?.rating, attempts, lastError, createdAt,
)
