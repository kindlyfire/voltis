package me.tijlvdb.voltis.domain.reading

import java.time.Instant
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ReadingStatus

// What the unsent ops will do, for display only (P2 §5, Projection): the server's rules applied
// here. Sends and `finishedAt` use the acknowledged state.

/**
 * What every lane the outbox writes to shows: its acknowledged state with [ops] applied in order.
 * A volume that starts its series shows on the series' lane, and a series op on the volumes its
 * guard covers. [lanes] holds the lanes that exist among the ops' own, their series and the covered
 * volumes; the result has an entry for each one an op touches, except a series it leaves as it was. A written time is its op's.
 */
fun project(ops: List<Op>, lanes: Map<String, Lane>): Map<String, Snapshot> = projected(ops, lanes).shown

/**
 * A lane's `last_read_at` and `status_updated_at` as a projection leaves them. An unsent op's change has no server
 * time yet, and the server will stamp it later than anything the phone knows: it is [lastReadRank] or [statusRank],
 * the op's place in the queue, and sorts after every server time. Otherwise the time is the server's, as the
 * acknowledged state, a restore's snapshot or a series' put-back status has it; null when it is cleared.
 */
data class Stamps(val lastReadAt: String?, val lastReadRank: Int?, val statusUpdatedAt: String?, val statusRank: Int?)

/** What [project] shows, and each lane's [Stamps]. */
class Projected(val shown: Map<String, Snapshot>, val stamps: Map<String, Stamps>)

fun projected(ops: List<Op>, lanes: Map<String, Lane>): Projected {
    val shown = mutableMapOf<String, Snapshot>()
    val stamps = mutableMapOf<String, Stamps>()
    fun stampsOf(id: String) = stamps[id] ?: lanes[id]?.state?.let { Stamps(it.lastReadAt, null, it.statusUpdatedAt, null) } ?: Stamps(null, null, null, null)
    /** [id]'s status became [next]: stamped when that changes it, to nothing included, as the server stamps it. */
    fun status(id: String, before: String?, next: String?, at: String?, rank: Int?) {
        if (next != before) stamps[id] = stampsOf(id).copy(statusUpdatedAt = at, statusRank = rank)
    }
    fun at(id: String?): Snapshot? = id?.let { shown[it] ?: lanes[it]?.state?.let { s -> Snapshot(s.status, s.progress, s.lastReadAt) } }

    /** reading.go's startSeries. */
    fun startSeries(seriesId: String?, reopen: Boolean, rank: Int) {
        val cur = at(seriesId) ?: return
        val starts = cur.status == null || cur.status == ReadingStatus.PLAN_TO_READ || (reopen && cur.status == ReadingStatus.COMPLETED)
        // Unchanged, it isn't shown: an entry overlays the reader's copy of the series, which may be newer.
        if (starts) {
            shown[seriesId!!] = cur.copy(status = ReadingStatus.READING)
            status(seriesId, cur.status, ReadingStatus.READING, null, rank)
        }
    }

    fun apply(lane: Lane, op: ReadingOp, at: String, rank: Int) {
        val id = lane.contentId
        val cur = at(id)!!
        val t = transition(cur, op, endOf(lane, op.progress), at)
        shown[id] = t.next
        when (op.op) {
            "position", "finish" -> if (t.write) stamps[id] = stampsOf(id).copy(lastReadAt = null, lastReadRank = rank)
            // The snapshot's time is one the server stamped.
            "restore" -> stamps[id] = stampsOf(id).copy(lastReadAt = op.snapshot?.lastReadAt, lastReadRank = null)
            // A full clear has no times at all.
            "clear" -> stamps[id] = Stamps(null, null, null, null)
        }
        if (op.op != "clear") status(id, cur.status, t.next.status, null, rank)
        if (startsSeries(op, t.outcome)) startSeries(lane.parentId, reopen = t.outcome == Outcome.STARTED || t.outcome == Outcome.MOVED_TO_READING, rank = rank)
    }

    /** A series' status put back, or set: it is written whatever the series' lane retains. [time]: the server's own, when it is put back. */
    fun seriesStatus(seriesId: String?, status: String?, time: String?, restored: Boolean, rank: Int) {
        val cur = at(seriesId) ?: return
        status(seriesId!!, cur.status, status, time.takeIf { restored }, rank.takeUnless { restored })
        shown[seriesId] = if (status == ReadingStatus.COMPLETED) cur.copy(status = status, progress = EmptyProgress) else cur.copy(status = status)
    }

    for ((rank, op) in ops.withIndex()) {
        val lane = lanes[op.contentId] ?: continue
        val time = Instant.ofEpochMilli(op.createdAt).toString()
        val volumes = op.guard?.guardVolumes().orEmpty().filter { it in lanes }
        when (op.kind) {
            OpKind.POSITION -> apply(lane, ReadingOp("position", progress = positionProgress(op.payload)), time, rank)
            OpKind.FINISH -> apply(lane, ReadingOp("finish", progress = op.payload), time, rank)
            OpKind.UNDO -> {
                val snapshot = AppJson.decodeFromJsonElement(Snapshot.serializer(), op.payload.getValue("snapshot"))
                apply(lane, ReadingOp("restore", snapshot = snapshot), time, rank)
                // The series gets back the status time it had, as the server's revertSeries does.
                (op.payload["series"] as? JsonObject)?.let { seriesStatus(lane.parentId, it.string("status"), it.string("status_updated_at"), restored = true, rank = rank) }
            }
            OpKind.COMMAND -> when (val name = op.payload.string("op")) {
                "series_status" -> {
                    val series = (op.payload["series"] as? JsonObject) ?: op.payload
                    seriesStatus(lane.parentId, series.string("status"), series.string("status_updated_at"), restored = "status_updated_at" in series, rank = rank)
                }
                else -> {
                    apply(lane, ReadingOp(name ?: continue, op.payload.string("status")), time, rank)
                    // A series clear, which clears its volumes with it.
                    for (id in volumes) {
                        shown[id] = Snapshot()
                        stamps[id] = Stamps(null, null, null, null)
                    }
                }
            }
            OpKind.SERIES -> {
                val seriesId = op.payload.string("series_id")
                // Completed at their end, keeping when they were last read (reading.go's markCompleted).
                for (id in volumes) {
                    val cur = at(id)!!
                    shown[id] = cur.copy(status = ReadingStatus.COMPLETED, progress = endOf(lanes.getValue(id), null))
                    status(id, cur.status, ReadingStatus.COMPLETED, null, rank)
                }
                when (op.payload.string("action")) {
                    SeriesAction.MARK_SERIES_COMPLETED -> seriesStatus(seriesId, ReadingStatus.COMPLETED, null, restored = false, rank = rank)
                    // Started, never completed.
                    SeriesAction.MARK_THROUGH -> if (volumes.isNotEmpty()) startSeries(seriesId, reopen = false, rank = rank)
                }
                // The op's own lane, unless it is the series and the action leaves its status alone.
                if (lane.contentId != seriesId || op.payload.string("action") == SeriesAction.MARK_SERIES_COMPLETED) {
                    shown.getOrPut(lane.contentId) { at(lane.contentId)!! }
                }
            }
        }
    }
    return Projected(shown, shown.keys.associateWith { stampsOf(it) })
}

/** Where [lane]'s item ends: `endProgress`, or without a known page count just the end. A series has none. */
private fun endOf(lane: Lane, supplied: JsonObject?): JsonObject = when {
    lane.type == ContentType.COMIC_SERIES || lane.type == ContentType.BOOK_SERIES -> EmptyProgress
    lane.type == ContentType.BOOK -> endProgress(ContentType.BOOK, 0, supplied)
    lane.pageCount != null -> endProgress(ContentType.COMIC, lane.pageCount)
    else -> JsonObject(mapOf("progress_percent" to JsonPrimitive(100), "at_end" to JsonPrimitive(true)))
}

private fun JsonObject.string(key: String) = (this[key] as? JsonPrimitive)?.contentOrNull
