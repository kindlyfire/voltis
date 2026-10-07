package me.tijlvdb.voltis.domain.comic

import me.tijlvdb.voltis.data.api.DisplayMetadata
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** The cases of `pages/read/ComicDisplay/pagedLayout.test.ts`. */
class PagedLayoutTest {
    private val p = PageDimensions(800, 1200)
    private val w = PageDimensions(1600, 1200)
    private val u = PageDimensions(0, 0)

    @Test
    fun buildSpreads() {
        data class Case(val name: String, val pages: List<PageDimensions>, val double: Boolean, val shift: Boolean, val expected: List<List<Int>>)
        val cases = listOf(
            Case("single mode", listOf(p, p, p), false, false, listOf(listOf(0), listOf(1), listOf(2))),
            Case("cover alone, then pairs", listOf(p, p, p, p, p), true, false, listOf(listOf(0), listOf(1, 2), listOf(3, 4))),
            Case("odd tail alone", listOf(p, p, p, p), true, false, listOf(listOf(0), listOf(1, 2), listOf(3))),
            Case(
                "wide page alone, leaving a single and restarting pairs",
                listOf(p, p, w, p, p, p),
                true,
                false,
                listOf(listOf(0), listOf(1), listOf(2), listOf(3, 4), listOf(5)),
            ),
            Case("shift pairs the cover", listOf(p, p, p, w), true, true, listOf(listOf(0, 1), listOf(2), listOf(3))),
            Case("unknown sizes as portrait", listOf(u, u, u), true, false, listOf(listOf(0), listOf(1, 2))),
        )
        for (case in cases) {
            val spreads = buildSpreads(case.pages, case.double, case.shift)
            assertEquals(case.name, case.expected, spreads)
            val of = spreadOfPages(spreads, case.pages.size)
            case.expected.forEachIndexed { i, spread -> spread.forEach { assertEquals(case.name, i, of[it]) } }
        }
    }

    @Test
    fun layoutSpread() {
        val portrait = Extent(400.0, 800.0)
        val landscape = Extent(1600.0, 900.0)
        data class Case(
            val name: String,
            val pages: List<PageDimensions>,
            val viewport: Extent,
            val fit: Fit,
            val zoomWide: Boolean,
            val height: Double,
            val width: Double,
        )
        val cases = listOf(
            // Zoomed: twice the viewport width, so each scroll edge shows one half.
            Case("portrait viewport zooms a wide page", listOf(w), portrait, Fit.Screen, true, 600.0, 800.0),
            Case("landscape viewport gains too little", listOf(w), landscape, Fit.Screen, true, 900.0, 1200.0),
            Case("gain just below 1.25", listOf(w), Extent(600.0, 562.0), Fit.Screen, true, 450.0, 600.0),
            Case("gain of exactly 1.25", listOf(w), Extent(600.0, 562.5), Fit.Screen, true, 562.5, 750.0),
            Case("zoomWide off", listOf(w), portrait, Fit.Screen, false, 300.0, 400.0),
            Case("fit width never zooms", listOf(w), portrait, Fit.Width, true, 300.0, 400.0),
            Case("fit height never zooms", listOf(w), portrait, Fit.Height, true, 800.0, 1066.67),
            Case("two pages share one height", listOf(p, p), landscape, Fit.Screen, true, 900.0, 1200.0),
        )
        for (case in cases) {
            val layout = checkNotNull(layoutSpread(case.pages, case.viewport, case.fit, case.zoomWide))
            assertEquals(case.name, case.height, layout.height, 0.005)
            assertEquals(case.name, case.width, layout.width, 0.005)
            assertEquals(case.name, case.width, layout.pages.sumOf { it.width }, 0.005)
        }
        // An unknown size.
        assertNull(layoutSpread(listOf(p, u), Extent(1000.0, 1000.0), Fit.Screen, zoomWide = true))
    }

    @Test
    fun scrollStep() {
        data class Case(val name: String, val pos: Double, val max: Double, val viewport: Double, val toEnd: Boolean, val expected: Double?)
        val cases = listOf(
            Case("steps by viewport minus overlap", 0.0, 3000.0, 1000.0, true, 900.0),
            Case("steps back", 2000.0, 3000.0, 1000.0, false, 1100.0),
            Case("overlap is at least 32px", 0.0, 3000.0, 200.0, true, 168.0),
            Case("snaps to the end", 2100.0, 3000.0, 1000.0, true, 3000.0),
            Case("snaps to the start", 900.0, 3000.0, 1000.0, false, 0.0),
            Case("null at the end", 2999.0, 3000.0, 1000.0, true, null),
            Case("null at the start", 0.0, 3000.0, 1000.0, false, null),
            // A zoomed wide page: max ≤ half width ≤ viewport.
            Case("wide page reaches the far half from the start", 0.0, 400.0, 400.0, true, 400.0),
            Case("wide page reaches the far half mid-pan", 150.0, 300.0, 400.0, true, 300.0),
            Case("wide page returns to the first half", 250.0, 300.0, 400.0, false, 0.0),
        )
        for (case in cases) assertEquals(case.name, case.expected, scrollStep(case.pos, case.max, case.viewport, case.toEnd))
    }

    @Test
    fun detectDirection() {
        fun meta(manga: String? = null, kind: String? = null) = DisplayMetadata(manga = manga, kind = kind)
        data class Case(val name: String, val comic: DisplayMetadata?, val series: DisplayMetadata?, val expected: ReadingDirection)
        val cases = listOf(
            Case("comic RTL tag", meta("YesAndRightToLeft"), meta("No"), ReadingDirection.Rtl),
            Case("comic No beats series kind", meta("no"), meta(kind = "manga"), ReadingDirection.Ltr),
            Case("comic Yes falls through to series", meta("Yes"), meta("yesandrighttoleft"), ReadingDirection.Rtl),
            Case("series No beats kind", meta("Unknown"), meta("No", "manga"), ReadingDirection.Ltr),
            Case("series kind manga", meta(), meta("Yes", "manga"), ReadingDirection.Rtl),
            Case("other kind", meta(), meta(kind = "comic"), ReadingDirection.Ltr),
            Case("nothing known", null, null, ReadingDirection.Ltr),
        )
        for (case in cases) assertEquals(case.name, case.expected, detectDirection(case.comic, case.series))
    }
}
