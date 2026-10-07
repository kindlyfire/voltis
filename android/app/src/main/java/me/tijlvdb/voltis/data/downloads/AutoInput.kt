package me.tijlvdb.voltis.data.downloads

import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.reading.effectiveReading
import me.tijlvdb.voltis.domain.downloads.AutoInput
import me.tijlvdb.voltis.domain.downloads.AutoRow
import me.tijlvdb.voltis.domain.downloads.AutoVolume
import me.tijlvdb.voltis.domain.downloads.SeriesPolicy

/**
 * What the rule reads for [seriesId] (P2 §17), all from [db]: null when it has no policy. Under the owner's
 * lock, inside the transaction that applies the plan; the reading engine's hold is read last, so a mutation
 * of the engine is either in Room already or set the hold before this read.
 */
internal suspend fun readAutoInput(
    db: VoltisDatabase,
    seriesId: String,
    pinned: (copyId: String) -> Boolean,
    readingHeld: () -> Boolean,
    /** Instead of the stored policy: the sheet's preview of one not saved yet. */
    policyOverride: SeriesPolicy? = null,
): AutoInput? {
    val policy = policyOverride ?: db.auto().policy(seriesId)?.let { SeriesPolicy(seriesId, it.keepNext, it.deleteFinished) } ?: return null
    val cached = db.content().volumes(seriesId)
    val rows = db.downloads().inSeries(seriesId).associateBy { it.contentId }
    val extra = rows.keys - cached.mapTo(HashSet()) { it.id }
    val extraRows = extra.chunked(500).flatMap { db.content().get(it) }.associateBy { it.id }
    val ids = cached.map { it.id } + extra
    val lanes = ids.chunked(500).flatMap { db.lanes().get(it) }.associateBy { it.contentId }
    // The one effective-state read: what the snapshots say, with the unsent ops over it. Shown is that; acked is the snapshot's own.
    val effective = db.effectiveReading(ids)
    val snapshots = ids.chunked(500).flatMap { db.snapshots().get(it) }.associate { it.contentId to it.status }
    fun volume(id: String, type: String, valid: Boolean, position: Int?): AutoVolume {
        val lane = lanes[id]
        val row = rows[id]
        return AutoVolume(
            id, type, valid, position,
            shown = effective[id]?.status,
            acked = snapshots[id],
            held = lane != null && (lane.needsReview || lane.rebase || !lane.acked),
            row = row?.let { AutoRow(it.state, it.copyId != null, it.keep, it.copyId?.let(pinned) == true) },
        )
    }
    val volumes = cached.map { volume(it.id, it.type, true, it.position) } +
        extra.sorted().map { id -> volume(id, extraRows[id]?.type ?: lanes[id]?.type ?: ContentType.COMIC, false, null) }
    val unsent = db.ops().unsentIn(seriesId)
    val offered = db.auto().offers(seriesId).toSet()
    return AutoInput(
        policy, volumes,
        ordered = cached.all { it.position != null }, unsent = unsent, readingHeld = readingHeld(), offered = offered,
    )
}

/** The status the server last stated of [id], from its snapshot: what "finished" means for a delete and for `keep`. */
internal suspend fun VoltisDatabase.ackedStatus(id: String): String? = snapshots().get(id)?.status
