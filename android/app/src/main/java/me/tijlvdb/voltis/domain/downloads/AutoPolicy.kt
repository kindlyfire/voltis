package me.tijlvdb.voltis.domain.downloads

// The rule of automatic downloads (P2 §17): pure, so it is the same under the owner's lock and in a plan made without it.

/** A series' policy: keep the next [keepNext] comic volumes downloaded (0: off), and delete finished ones when [deleteFinished]. */
data class SeriesPolicy(val seriesId: String, val keepNext: Int, val deleteFinished: Boolean)

data class AutoVolume(
    val id: String,
    val type: String,
    /** False: a downloaded volume gone from the series' list. */
    val valid: Boolean,
    val position: Int?,
    /** The projection, with unsent ops applied: the lane's `shown_status`, else the cached row's status. */
    val shown: String?,
    /** The acknowledged status: the lane's, else the cached row's (no lane means nothing is unsent). */
    val acked: String?,
    /** The lane is `needs_review`, `rebase` or not acknowledged. */
    val held: Boolean,
    val row: AutoRow?,
)

data class AutoRow(val state: String, val hasCopy: Boolean, val keep: Boolean, val pinned: Boolean)

data class AutoInput(
    val policy: SeriesPolicy,
    /** The cached valid volumes in the server's order, then every other volume that has a download row of this series. */
    val volumes: List<AutoVolume>,
    /** Every valid volume has a position. */
    val ordered: Boolean,
    /** Any op on the series' lane or on one of its volumes' lanes. */
    val unsent: Boolean,
    /** The reading engine holds a mutation that isn't stored. */
    val readingHeld: Boolean,
    val offered: Set<String>,
)

data class AutoPlan(val queue: List<String>, val offer: Set<String>, val delete: List<String>) {
    val isEmpty get() = queue.isEmpty() && offer.isEmpty() && delete.isEmpty()
}

private const val COMIC = "comic"
private const val READING = "reading"
private const val COMPLETED = "completed"
private const val DROPPED = "dropped"

/**
 * The volumes to queue and to delete so that the series' downloads match its policy. The window is
 * the first `keepNext` comics from the anchor on (the last valid volume that is reading or completed,
 * else the first) that are neither completed nor dropped; the one in progress counts. Queued: window
 * volumes without a row that were never offered. Offered: every window volume not offered yet.
 */
fun autoPlan(input: AutoInput): AutoPlan {
    val policy = input.policy
    var queue = emptyList<String>()
    var offer = emptySet<String>()
    if (policy.keepNext > 0 && input.ordered) {
        val valid = input.volumes.filter { it.valid }
        val anchor = valid.indexOfLast { it.shown == READING || it.shown == COMPLETED }.coerceAtLeast(0)
        val window = valid.drop(anchor).filter { it.type == COMIC && it.shown != COMPLETED && it.shown != DROPPED }.take(policy.keepNext)
        queue = window.filter { it.row == null && it.id !in input.offered }.map { it.id }
        offer = window.mapTo(LinkedHashSet()) { it.id } - input.offered
    }
    val delete = if (policy.deleteFinished && !input.readingHeld && !input.unsent) {
        input.volumes.filter { v ->
            val row = v.row
            row != null && row.state == DownloadState.DONE && row.hasCopy && v.acked == COMPLETED && !v.held && !row.keep && !row.pinned
        }.map { it.id }
    } else {
        emptyList()
    }
    return AutoPlan(queue, offer, delete)
}
