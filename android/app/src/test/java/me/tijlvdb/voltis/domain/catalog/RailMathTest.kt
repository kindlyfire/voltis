package me.tijlvdb.voltis.domain.catalog

import me.tijlvdb.voltis.data.api.ContentBuckets
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RailMathTest {
    @Test
    fun segmentsAreRunsInListOrder() {
        val buckets = ContentBuckets(10, listOf(ContentBuckets.Bucket(null, 3), ContentBuckets.Bucket("a", 5), ContentBuckets.Bucket("b", 2)))
        assertEquals(listOf(Segment(null, 0), Segment("a", 3), Segment("b", 8)), segmentsOf(buckets))
        assertEquals(emptyList<Segment>(), segmentsOf(ContentBuckets(10)))
    }

    @Test
    fun positionsAndSeeks() {
        // 250 rows of 100 px under 500 px: 245 rows scroll.
        val rail = RailGeometry(1000, 4, 100f, 500f)
        val segments = listOf(Segment("a", 0), Segment("b", 400), Segment("c", 800), Segment("d", 1000))
        val starts = rail.starts(segments)
        assertEquals(listOf(0f, 100 / 245f, 200 / 245f, 1f), starts)
        assertEquals(2, segmentAt(starts, 0.9f))
        assertEquals(3, segmentAt(starts, 1f))
        assertEquals(0, segmentAt(starts, -0.1f))

        // A start lands on its first card, whole; the ends land on the top and the last rows.
        assertEquals(RailSeek(0, 0), rail.seek(0f))
        assertEquals(RailSeek(400, 0), rail.seek(starts[1]))
        assertEquals(RailSeek(800, 0), rail.seek(starts[2]))
        assertEquals(RailSeek(980, 0), rail.seek(1f))
        // Inside a segment it is proportional, and the position reads back.
        assertEquals(RailSeek(488, 50), rail.seek(0.5f))
        assertEquals(0.5f, rail.value(488, 50f), 1e-6f)
        assertEquals(0f, rail.value(0, -30f), 0f)
        assertEquals(1f, rail.value(990, 0f), 0f)
        // With a header the position is continuous across its end: each landing is its requested pixel, give or take rounding.
        val tall = RailGeometry(1000, 4, 100f, 500f, header = 200f)
        assertEquals(100 / 24700f, tall.value(0, 100f), 1e-6f)
        assertEquals(0.5f, tall.value(tall.seek(0.5f).index, tall.seek(0.5f).offset.toFloat()), 1e-3f)
        fun px(seek: RailSeek) = seek.index / 4 * 100 + seek.offset
        val landed = listOf(99, 199, 200, 201, 299).map { y ->
            px(tall.seek(y / 24700f)).also { assertEquals(y.toFloat(), it.toFloat(), 1f) }
        }
        assertEquals(landed.sorted(), landed)
        assertEquals(RailSeek(0, 99), tall.seek(99 / 24700f))
        assertEquals(RailSeek(8, 1), tall.seek(201 / 24700f))
        // A header taller than the viewport: the bottom is reachable, and the landings keep rising to it.
        val high = RailGeometry(1000, 4, 100f, 500f, header = 650f)
        assertEquals(RailSeek(996, 250), high.seek(1f))
        fun top(seek: RailSeek) = seek.index / 4 * 100 + seek.offset
        assertEquals(25150f, top(high.seek(1f)).toFloat(), 1f)
        assertEquals(24999f, top(high.seek(24999 / 25150f)).toFloat(), 1f)
        assertEquals(25100f, top(high.seek(25100 / 25150f)).toFloat(), 1f)
        // Shown for more than 3 viewports and 2 segments; less than a viewport still scrolls by a row.
        assertTrue(rail.shown(2))
        assertFalse(rail.shown(1))
        assertFalse(RailGeometry(40, 4, 100f, 500f).shown(5))
        assertEquals(RailSeek(4, 0), RailGeometry(8, 4, 100f, 500f).seek(1f))
    }
}
