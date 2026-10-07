package me.tijlvdb.voltis.domain.comic

import kotlin.math.max
import kotlin.math.min

// A port of `pages/read/ComicDisplay/pagedLayout.ts`, pinned by WebSourcePinTest.

/** How much bigger half a wide page must get before zooming into it is worth a scroll. */
private const val ZOOM_MIN_GAIN = 1.25

val PageDimensions.sized: Boolean get() = width > 0 && height > 0

private val PageDimensions.wide: Boolean get() = sized && width > height

/**
 * Groups pages into screens of ascending page indices. In double mode the cover and wide pages
 * stand alone (unless [shift] pairs the cover), and pairing restarts after a wide page. Unknown
 * sizes count as portrait.
 */
fun buildSpreads(pages: List<PageDimensions>, double: Boolean, shift: Boolean): List<List<Int>> {
    if (!double) return pages.indices.map { listOf(it) }
    fun pairable(i: Int) = i < pages.size && !pages[i].wide && (i > 0 || shift)
    val spreads = mutableListOf<List<Int>>()
    var i = 0
    while (i < pages.size) {
        if (pairable(i) && pairable(i + 1)) {
            spreads += listOf(i, i + 1)
            i += 2
        } else {
            spreads += listOf(i++)
        }
    }
    return spreads
}

/** Maps each page to the index of the spread holding it. */
fun spreadOfPages(spreads: List<List<Int>>, pageCount: Int): List<Int> {
    val out = IntArray(pageCount)
    spreads.forEachIndexed { i, spread -> spread.forEach { out[it] = i } }
    return out.asList()
}

data class Extent(val width: Double, val height: Double)

data class SpreadLayout(val pages: List<Extent>, val width: Double, val height: Double)

/** Sizes a spread in px, all pages sharing one height. Null when a page size is unknown. */
fun layoutSpread(pages: List<PageDimensions>, viewport: Extent, fit: Fit, zoomWide: Boolean): SpreadLayout? {
    if (pages.isEmpty() || !pages.all { it.sized }) return null
    val (vw, vh) = viewport
    val aspect = pages.sumOf { it.width.toDouble() / it.height }

    var h = when (fit) {
        Fit.Height -> vh
        Fit.Width -> vw / aspect
        Fit.Screen -> min(vh, vw / aspect)
    }
    // Wide zoom: one wide page shown half at a time, overflowing horizontally.
    if (fit == Fit.Screen && zoomWide && pages.size == 1 && pages[0].wide) {
        val half = min(vh, vw / (aspect / 2))
        if (half >= ZOOM_MIN_GAIN * h) h = half
    }

    return SpreadLayout(pages.map { Extent(h * it.width / it.height, h) }, h * aspect, h)
}

/**
 * The next scroll target along one axis toward the reading-order end ([toEnd]) or start, or null
 * when already at that edge. Snaps to the edge when the rest fits in about one step.
 */
fun scrollStep(pos: Double, max: Double, viewport: Double, toEnd: Boolean): Double? {
    val step = viewport - max(32.0, 0.1 * viewport)
    val remaining = if (toEnd) max - pos else pos
    if (remaining <= 2) return null
    if (remaining <= viewport) return if (toEnd) max else 0.0
    return if (toEnd) pos + step else pos - step
}
