package me.tijlvdb.voltis.domain.reading

import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ReadingStatus

// The port of transition, startsSeries and endProgress in backend/routes/reading.go, held to it by
// the table both TransitionTest and TestReadingTransition run.

/** An event or command as `transition` takes it: [status] for `set_status`, [snapshot] for `restore`. */
data class ReadingOp(
    val op: String,
    val status: String? = null,
    val progress: JsonObject? = null,
    val snapshot: Snapshot? = null,
)

/** [previous] is what Undo restores, set only when reading overrode a deliberate status. */
data class Transition(val next: Snapshot, val outcome: String, val previous: Snapshot?, val write: Boolean)

private fun isHeld(status: String?) =
    status == ReadingStatus.PLAN_TO_READ || status == ReadingStatus.ON_HOLD || status == ReadingStatus.DROPPED

/** The next state of an item at [cur] after [op]. [end] is the item's [endProgress]; [now] the time it is written. */
fun transition(cur: Snapshot, op: ReadingOp, end: JsonObject, now: String): Transition {
    var previous: Snapshot? = null
    val (next, outcome) = when (op.op) {
        "position" -> {
            val moved = cur.copy(progress = op.progress ?: EmptyProgress, lastReadAt = now)
            when {
                cur.status == null -> moved.copy(status = ReadingStatus.READING) to Outcome.STARTED
                isHeld(cur.status) -> {
                    previous = cur
                    moved.copy(status = ReadingStatus.READING) to Outcome.MOVED_TO_READING
                }
                // Reading keeps its status, and so does a completed item being read again.
                else -> moved to Outcome.SAVED
            }
        }
        "finish" -> {
            if (cur.status == ReadingStatus.COMPLETED) return Transition(cur, Outcome.NONE, null, false)
            if (isHeld(cur.status)) previous = cur
            Snapshot(ReadingStatus.COMPLETED, end, now) to Outcome.COMPLETED
        }
        "set_status" -> {
            if (op.status == ReadingStatus.COMPLETED) return transition(cur, ReadingOp("mark_completed"), end, now)
            cur.copy(status = op.status) to Outcome.STATUS_SET
        }
        "mark_completed" -> cur.copy(status = ReadingStatus.COMPLETED, progress = end) to Outcome.COMPLETED
        "clear" -> Snapshot() to Outcome.CLEARED
        "restore" -> checkNotNull(op.snapshot) to Outcome.RESTORED
        else -> throw IllegalArgumentException("unknown op ${op.op}")
    }
    return Transition(next, outcome, previous, true)
}

/** A position's progress as the server stores it: only a finish ends an item, so `at_end` is dropped. */
fun positionProgress(progress: JsonObject) = JsonObject(progress - "at_end")

/** Whether [outcome] moves the item into reading or completed, which starts its series. */
fun startsSeries(op: ReadingOp, outcome: String) = when (outcome) {
    Outcome.STARTED, Outcome.MOVED_TO_READING, Outcome.COMPLETED -> true
    Outcome.STATUS_SET -> op.status == ReadingStatus.READING
    else -> false
}

/** Finished content's progress: a comic's last page, or a book's end (at the reader's finishing locator in [supplied]'s `book`). A series has none. */
fun endProgress(type: String, pages: Int, supplied: JsonObject? = null): JsonObject = when (type) {
    ContentType.COMIC -> JsonObject(
        mapOf(
            "current_page" to JsonPrimitive(maxOf(pages - 1, 0)),
            "progress_percent" to JsonPrimitive(100),
            "at_end" to JsonPrimitive(true),
        ),
    )
    ContentType.BOOK -> JsonObject(
        buildMap {
            put("progress_percent", JsonPrimitive(100))
            put("at_end", JsonPrimitive(true))
            supplied?.get("book")?.takeIf { it !is JsonNull }?.let { put("book", it) }
        },
    )
    else -> EmptyProgress
}
