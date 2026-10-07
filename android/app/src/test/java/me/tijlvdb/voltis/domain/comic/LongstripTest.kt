package me.tijlvdb.voltis.domain.comic

import me.tijlvdb.voltis.ui.reader.StripAnchor
import me.tijlvdb.voltis.ui.reader.anchorAt
import me.tijlvdb.voltis.ui.reader.spotOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class LongstripTest {
    private val tall = PageDimensions(800, 12000)

    @Test
    fun itemSizes() {
        data class Case(val page: PageDimensions, val percent: Int, val density: Float, val expected: StripItem)
        val cases = listOf(
            // The viewport's share, capped at the page's own width in dp.
            Case(PageDimensions(2000, 3000), 100, 1f, StripItem(0, 1080, 1620)),
            Case(PageDimensions(2000, 3000), 50, 1f, StripItem(0, 540, 810)),
            Case(PageDimensions(800, 1200), 100, 1f, StripItem(0, 800, 1200)),
            Case(PageDimensions(800, 4096), 100, 1f, StripItem(0, 800, 4096)),
            // On a dense screen an 800 px page fills the width, and a 300 px one stops at 300 dp.
            Case(PageDimensions(800, 1200), 100, 2.625f, StripItem(0, 1080, 1620)),
            Case(PageDimensions(300, 450), 100, 2.625f, StripItem(0, 788, 1182)),
            // An unknown size takes the share until its file gives one.
            Case(PageDimensions(0, 0), 50, 1f, StripItem(0, 540, 0)),
        )
        for (case in cases) {
            val strip = layoutStrip(listOf(case.page), 1080, 1920, case.percent, case.density)
            assertEquals(case.toString(), listOf(case.expected), strip.items)
        }
    }

    @Test
    fun tallPagesBecomeBands() {
        // At 1:1: seven bands of at most a screen, each decoded two rows past its slice.
        val strip = layoutStrip(listOf(PageDimensions(800, 1200), tall, tall), 1080, 1920, 100, 1f)
        assertEquals(listOf(0, 1, 8), strip.firstItem)
        assertEquals(15, strip.items.size)
        val bands = strip.items.subList(1, 8)
        assertEquals(listOf(1714, 1714, 1714, 1715, 1714, 1714, 1715), bands.map { it.height })
        assertEquals(listOf(0, 1712, 3426, 5140, 6855, 8569, 10283), bands.map { it.band!!.srcTop })
        assertEquals(listOf(1716, 3430, 5144, 6859, 8573, 10287, 12000), bands.map { it.band!!.srcBottom })
        assertEquals(listOf(0f) + List(6) { -2f }, bands.map { it.band!!.offset })
        assertTrue(bands.all { it.band!!.run { sample == 1 && scale == 1f } })

        data class Case(val page: PageDimensions, val percent: Int, val density: Float, val sample: Int)
        val cases = listOf(
            // Scaled down, on a sampled grid, and scaled up.
            Case(tall, 50, 1f, 1),
            Case(PageDimensions(1000, 9001), 100, 1f, 1),
            Case(PageDimensions(1440, 30011), 35, 1f, 2),
            Case(PageDimensions(2400, 40001), 35, 1f, 4),
            Case(tall, 100, 2.625f, 1),
        )
        for (case in cases) {
            val (page, percent, density, sample) = case
            val items = layoutStrip(listOf(page), 1080, 1920, percent, density).items
            val drawn = items.map { it.band!! }
            val height = items.sumOf { it.height }
            val rows = page.height.toDouble() / height
            assertEquals(case.toString(), Math.round(items[0].width.toDouble() * page.height / page.width).toInt(), height)
            assertTrue(case.toString(), items.all { it.width == items[0].width && it.height in 1..1920 })
            // One sampling grid and one scale for the page.
            assertTrue(case.toString(), drawn.all { it.sample == sample && it.srcTop % sample == 0 && it.scale == drawn[0].scale })
            assertEquals(case.toString(), sample / rows, drawn[0].scale.toDouble(), 1e-4)
            // The decoded rows tile the page, each band reaching into its neighbours.
            assertEquals(case.toString(), 0, drawn.first().srcTop)
            assertEquals(case.toString(), page.height, drawn.last().srcBottom)
            drawn.zipWithNext { a, b -> assertTrue(case.toString(), a.srcBottom - b.srcTop >= 2 * BAND_OVERLAP * sample) }
            // Each band's first row is drawn where the page has it, and its rows cover its item with the overlap to spare.
            var top = 0
            for ((item, band) in items.zip(drawn)) {
                assertEquals(case.toString(), band.srcTop / rows - top, band.offset.toDouble(), 1e-2)
                val spareAbove = -band.offset / band.scale
                val spareBelow = ((band.srcBottom - band.srcTop) / sample * band.scale + band.offset - item.height) / band.scale
                assertTrue("$case $band", band.srcTop == 0 || spareAbove >= BAND_OVERLAP)
                assertTrue("$case $band", band.srcBottom == page.height || spareBelow >= BAND_OVERLAP - 1)
                top += item.height
            }
        }
    }

    @Test
    fun pageAtTheCentre() {
        // Items: page 0, the seven bands of page 1, page 2.
        val strip = layoutStrip(listOf(PageDimensions(800, 1200), tall, PageDimensions(800, 1200)), 1080, 1920, 100, 1f)
        data class Case(val first: Int, val tops: List<Int>, val expected: Int?)
        val cases = listOf(
            Case(0, listOf(0, 1200), 0),
            // The last item starting at or above the centre.
            Case(0, listOf(-400, 800, 2514), 1),
            Case(0, listOf(-239, 961), 0),
            Case(0, listOf(-240, 960), 1),
            // Any band of a page is that page.
            Case(4, listOf(-1000, 714), 1),
            Case(7, listOf(-900, 815, 2015), 2),
            // Nothing above the centre yet: the first visible item.
            Case(8, listOf(1500), 2),
            // The end card, past the pages, is the last page.
            Case(8, listOf(-1100, 100), 2),
            Case(0, emptyList(), null),
        )
        for (case in cases) assertEquals(case.toString(), case.expected, strip.pageAt(case.first, case.tops, 960))
        assertNull(layoutStrip(emptyList(), 1080, 1920, 100, 1f).pageAt(0, listOf(0), 960))
    }

    @Test
    fun aPlaceInAPageSurvivesANewLayout() {
        val pages = listOf(PageDimensions(800, 1200), tall, PageDimensions(800, 1200))
        val portrait = layoutStrip(pages, 1080, 1920, 100, 1f)
        val landscape = layoutStrip(pages, 1920, 1080, 100, 1f)
        // 300 px into the fourth band of the tall page, as a rotation leaves it.
        val anchor = portrait.anchorAt(4, 300, placement = 2)!!
        assertEquals(1, anchor.page)
        assertEquals(2, anchor.placement)
        val spot = landscape.spotOf(anchor)!!
        assertEquals(1, landscape.items[spot.index].page)
        // The same share of the page, within a px, in a layout of other bands.
        val back = landscape.anchorAt(spot.index, spot.offset, placement = 2)!!
        assertEquals(anchor.fraction, back.fraction, 1.0 / landscape.items.filter { it.page == 1 }.sumOf { it.height })
        // A rotation before the page's size is read from its file: no place in it, so it restores to its top (accepted).
        val unsized = layoutStrip(listOf(PageDimensions(800, 1200), PageDimensions(0, 0)), 1920, 1080, 100, 1f)
        assertNull(unsized.spotOf(anchor))
        // The list opens on the page's first item, and an anchor past the last page has no place.
        assertEquals(1, unsized.firstItem[anchor.page])
        assertNull(unsized.spotOf(StripAnchor(9, 0.5, 2)))
        assertEquals(1, layoutStrip(pages, 1920, 1080, 100, 1f).spotOf(anchor)!!.let { landscape.items[it.index].page })
        // A page without a size has no place to keep.
        assertNull(layoutStrip(listOf(PageDimensions(0, 0)), 1080, 1920, 100, 1f).anchorAt(0, 0, 0))
    }
}
