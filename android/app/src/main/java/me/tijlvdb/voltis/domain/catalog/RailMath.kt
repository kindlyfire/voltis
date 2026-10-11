package me.tijlvdb.voltis.domain.catalog

import kotlin.math.ceil
import kotlin.math.floor
import kotlin.math.max
import me.tijlvdb.voltis.data.api.ContentBuckets

/** A run of the list the scroll rail jumps to: the [key] of its rows' sort (null: rows without one), and the index of its [first] row. */
data class Segment(val key: String?, val first: Int)

/** The segments of [buckets]: one per bucket, in list order. */
fun segmentsOf(buckets: ContentBuckets): List<Segment> {
    var first = 0
    return buckets.buckets.map { bucket ->
        Segment(bucket.key, first).also { first += bucket.count }
    }
}

/** Where a rail seek lands: card [index] scrolled [offset] px further down. */
data class RailSeek(val index: Int, val offset: Int)

/**
 * The scroll rail's math over [count] cards in [columns] columns of rows [rowHeight] px high, under a [header] px high,
 * which is the grid's top padding, in a [viewport] px high (less its end padding). A position is a fraction of the
 * scrollable px, header included, as the web's is of the page.
 */
data class RailGeometry(
    private val count: Int,
    private val columns: Int,
    private val rowHeight: Float,
    private val viewport: Float,
    private val header: Float = 0f,
) {
    private val rows = ceil(count / columns.toFloat())
    private val total = header + rows * rowHeight
    private val scrollable = max(rowHeight, total - viewport)

    /** The rail shows for a list of more than 3 viewports and at least 2 segments. */
    fun shown(segments: Int) = segments >= 2 && total > 3 * viewport

    /** The scroll position of a viewport whose top is [above] px into the row of card [index], from the grid's content start. */
    fun value(index: Int, above: Float): Float = ((index / columns * rowHeight + above) / scrollable).coerceIn(0f, 1f)

    /** The segments' positions: the first is 0, so it also covers the header. */
    fun starts(segments: List<Segment>): List<Float> =
        segments.mapIndexed { i, s -> if (i == 0) 0f else ((header + s.first / columns * rowHeight) / scrollable).coerceAtMost(1f) }

    fun seek(position: Float): RailSeek {
        val y = position.coerceIn(0f, 1f) * scrollable
        // A start is a whole row, up to rounding. Past the last row the offset goes on, which a header taller than the viewport needs.
        val row = floor(y / rowHeight + 1e-3f).toInt().coerceAtMost((count - 1) / columns)
        return RailSeek(row * columns, (y - row * rowHeight).coerceAtLeast(0f).toInt())
    }
}

/** The last of [starts] that is at or before [position]. */
fun segmentAt(starts: List<Float>, position: Float): Int = max(0, starts.indexOfLast { it <= position })
