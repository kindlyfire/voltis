package me.tijlvdb.voltis.ui.downloads

import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.domain.catalog.MAX_BULK_DOWNLOADS
import me.tijlvdb.voltis.domain.downloads.DownloadState

/** How many volumes the "Next" shortcut takes, the current one included. */
const val NEXT_COUNT = 5

private val DONE_WITH = setOf(ReadingStatus.COMPLETED, ReadingStatus.DROPPED)

/**
 * The download dialog's range: positions in the series' volume list, both ends included. Both null
 * is no range; [to] is never before [from], and equal ends are a one-volume range.
 */
data class RangeSel(val from: Int? = null, val to: Int? = null)

enum class Shortcut { All, Unread, Next }

/** The first volume that isn't completed or dropped, or null. */
fun firstUnread(volumes: List<Content>): Int? = volumes.indexOfFirst { it.userData?.status !in DONE_WITH }.takeIf { it >= 0 }

/** Where reading is: the continue target if it is listed, else the first unread, else the first volume. */
fun currentIndex(volumes: List<Content>, continueId: String?): Int? =
    if (volumes.isEmpty()) null else volumes.indexOfFirst { it.id == continueId }.takeIf { it >= 0 } ?: firstUnread(volumes) ?: 0

/** From the current volume to the last. */
fun defaultRange(volumes: List<Content>, current: Int?) = if (current == null) RangeSel() else RangeSel(current, volumes.lastIndex)

/** The range a shortcut fills, or null when it has none (no unread volume, nothing listed). */
fun shortcutRange(shortcut: Shortcut, volumes: List<Content>, current: Int?): RangeSel? = when (shortcut) {
    Shortcut.All -> if (volumes.isEmpty()) null else RangeSel(0, volumes.lastIndex)
    Shortcut.Unread -> firstUnread(volumes)?.let { RangeSel(it, volumes.lastIndex) }
    Shortcut.Next -> current?.let { RangeSel(it, minOf(it + NEXT_COUNT - 1, volumes.lastIndex)) }
}

/** What the To picker offers: only volumes after [from]. */
fun toOptions(volumes: List<Content>, from: Int?): IntRange = if (from == null) IntRange.EMPTY else from + 1..volumes.lastIndex

/** Picking From keeps To while it is after it, else To goes back to the last volume. */
fun RangeSel.withFrom(index: Int, volumes: List<Content>) = RangeSel(index, to?.takeIf { it > index } ?: volumes.lastIndex)

/** Picking To at or before From changes nothing. */
fun RangeSel.withTo(index: Int) = if (from != null && index > from) copy(to = index) else this

/**
 * What the range downloads: the volumes in it that have no download, in order. The store's enqueue
 * leaves a volume that already has a row alone, whatever its state, so a paused or failed one is not
 * retried by this download: [present] counts the rows that are downloaded, running or queued, and
 * [stuck] those paused or failed.
 */
class RangePlan(val items: List<Content>, val present: Int, val stuck: Int) {
    val count get() = items.size
    val bytes get() = items.sumOf { it.fileSize ?: 0L }

    /** Some volume has no size, so [bytes] is a lower bound. */
    val sizeUnknown get() = items.any { it.fileSize == null }
    val tooMany get() = count > MAX_BULK_DOWNLOADS
}

/** [states] is the download state of each volume that has a row, by id. */
fun rangePlan(volumes: List<Content>, sel: RangeSel, states: Map<String, String>): RangePlan {
    val from = sel.from ?: return RangePlan(emptyList(), 0, 0)
    val inRange = volumes.subList(from.coerceIn(0, volumes.size), ((sel.to ?: from) + 1).coerceIn(0, volumes.size))
    val (items, have) = inRange.partition { it.id !in states }
    val stuck = have.count { states[it.id] == DownloadState.PAUSED || states[it.id] == DownloadState.FAILED }
    return RangePlan(items, have.size - stuck, stuck)
}
