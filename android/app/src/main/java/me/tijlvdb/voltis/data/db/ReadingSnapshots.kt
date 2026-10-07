package me.tijlvdb.voltis.data.db

import kotlinx.serialization.json.JsonObject
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.reading.toOp
import me.tijlvdb.voltis.data.storage.localTransaction
import me.tijlvdb.voltis.domain.reading.ReadingSnapshot
import me.tijlvdb.voltis.domain.reading.references
import me.tijlvdb.voltis.domain.reading.ReadingState

/**
 * The import primitive: every reading state the server sends goes through here before anything else
 * sees it. A snapshot is inserted when its content has none, replaces the stored one only when its
 * `reading_seq` is greater, and is ignored otherwise (an equal seq is the same state). The writes
 * commute and repeat harmlessly, so arrival order, request timing, restarts and clocks don't matter.
 * Join a caller's transaction, or make one.
 */
suspend fun VoltisDatabase.importReading(states: Collection<ReadingSnapshot>) {
    if (states.isEmpty()) return
    localTransaction {
        val dao = snapshots()
        for ((id, s) in states) {
            dao.upsertNewer(id, s.seq, s.revision, s.status, s.statusUpdatedAt, AppJson.encodeToString(JsonObject.serializer(), s.progress), s.progressUpdatedAt, s.lastReadAt)
        }
    }
}

fun ReadingSnapshotEntity.toState() = ReadingState(
    revision, status, statusUpdatedAt, AppJson.decodeFromString(JsonObject.serializer(), progress), progressUpdatedAt, lastReadAt, seq,
)

/**
 * What the unsent ops still read or write (their content, the volumes their guards cover, their series), whatever
 * becomes of the content rows and lanes: the outbox's projection and its late answers need their snapshots, and
 * their lanes' types and page counts. Read inside the caller's transaction.
 */
suspend fun VoltisDatabase.opReferences(): Set<String> {
    val ops = ops().all().map { it.toOp() }
    val parents = ops.map { it.contentId }.distinct().chunked(500).flatMap { lanes().get(it) }.associate { it.contentId to it.parentId }
    return ops.flatMap { it.references(parents[it.contentId]) }.toSet()
}

/** Deletes the lanes untouched since [before] that nothing needs: a lane an unsent op covers by its guard stays, as its row and snapshot might not. */
suspend fun VoltisDatabase.pruneLanes(before: Long) = localTransaction {
    val keep = opReferences()
    lanes().idle(before).filter { it !in keep }.chunked(500).forEach { lanes().delete(it) }
}

/** Deletes the snapshots no content row, download, policy, held review or unsent op (its content or its guarded ids) needs. Lanes don't keep them. */
suspend fun VoltisDatabase.pruneSnapshots() = localTransaction {
    val keep = opReferences()
    snapshots().unneeded().filter { it !in keep }.chunked(500).forEach { snapshots().delete(it) }
}
