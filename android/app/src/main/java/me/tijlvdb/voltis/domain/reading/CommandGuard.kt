package me.tijlvdb.voltis.domain.reading

import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import me.tijlvdb.voltis.data.api.ReadingStatus

// What a command covers and was made against, and whether that moved since (P2 §5, Guards).

/** `series-reading`'s actions; `clear` on a series goes as a plain `clear` command with a guard. */
object SeriesAction {
    const val MARK_THROUGH = "mark_through"
    const val MARK_SERIES_COMPLETED = "mark_series_completed"
    const val CLEAR = "clear"
}

/**
 * The volumes a series command writes, picked as `seriesReading` (reading.go) picks them from the
 * list in the server's order. Null when [untilId] isn't listed.
 */
fun covered(volumes: List<VolumeRef>, action: String, includeUnread: Boolean = false, untilId: String? = null): List<String>? {
    fun unfinished(of: List<VolumeRef>) =
        of.filter { it.userData?.status != ReadingStatus.COMPLETED && it.userData?.status != ReadingStatus.DROPPED }.map { it.id }
    return when (action) {
        SeriesAction.MARK_THROUGH -> volumes.indexOfFirst { it.id == untilId }.takeIf { it >= 0 }?.let { unfinished(volumes.take(it + 1)) }
        SeriesAction.MARK_SERIES_COMPLETED -> if (includeUnread) unfinished(volumes) else emptyList()
        else -> volumes.map { it.id }
    }
}

/** `{series: revision, volumes: {id: revision}}`: the series' revision and each of the [covered] volumes' when the command was made. */
fun guardOf(volumes: SeriesVolumes, covered: List<String>): JsonObject {
    fun revision(value: String?) = value?.let(::JsonPrimitive) ?: JsonNull
    val revisions = volumes.volumes.associate { it.id to it.userData?.revision }
    return JsonObject(mapOf("series" to revision(volumes.revision), "volumes" to JsonObject(covered.associateWith { revision(revisions[it]) })))
}

/** The IDs of the volumes a guard covers, in the list's order. */
fun JsonObject.guardVolumes(): Set<String> = (this["volumes"] as? JsonObject)?.keys.orEmpty()

/** What a dropped command was, as a notice names it (`NoticeDetail.command`). */
object SyncCommand {
    const val CLEAR = "clear"
    const val MARK_COMPLETED = "mark_completed"
    const val MARK_THROUGH = SeriesAction.MARK_THROUGH
    const val MARK_SERIES_COMPLETED = SeriesAction.MARK_SERIES_COMPLETED
    const val UNDO = "undo"
}

/** `clear`, `mark_completed`, and `set_status` to completed: they replace what the item has, so a change elsewhere drops them. */
fun replaces(request: JsonObject): Boolean {
    val op = request.text("op")
    return op == "clear" || op == "mark_completed" || (op == "set_status" && request.text("status") == ReadingStatus.COMPLETED)
}

/**
 * Whether the series or a volume a series command covers moved since its [guard] was taken: its
 * revision is another, and [ours] says we didn't write it. [covered] are the volumes it covers in
 * [now]; one the guard doesn't have is compared against null. Volumes no longer listed aren't written.
 */
fun guardMoved(guard: JsonObject, now: SeriesVolumes, covered: List<String>, ours: (revision: String?) -> Boolean): Boolean {
    fun moved(then: String?, current: String?) = current != then && !ours(current)
    if (moved(guard.text("series"), now.revision)) return true
    val then = guard["volumes"] as? JsonObject ?: JsonObject(emptyMap())
    val revisions = now.volumes.associate { it.id to it.userData?.revision }
    return (then.keys + covered).any { id -> id in revisions && moved(then.text(id), revisions[id]) }
}

private fun JsonObject.text(key: String) = (this[key] as? JsonPrimitive)?.contentOrNull
