package me.tijlvdb.voltis.domain.comic

import kotlin.math.ceil
import kotlin.math.floor
import kotlin.math.min
import kotlin.math.roundToInt

/** A page decoded taller than this is shown as bands: it would be too large for one bitmap. */
const val MAX_STRIP_PAGE_HEIGHT = 4096

/** Sampled rows a band is decoded beyond its slice on each side, so filtering at its edges has its neighbours' rows. */
const val BAND_OVERLAP = 2

/**
 * A horizontal slice of a page. It is decoded from source rows [srcTop] to [srcBottom]
 * (exclusive) at [sample], which reach past the slice by [BAND_OVERLAP] rows and start on the
 * page's sampling grid. Every band of a page is drawn with the same [scale], px per decoded row,
 * its first row at [offset] px from the item's top: one picture, cut by the items' bounds.
 */
data class Band(val srcTop: Int, val srcBottom: Int, val sample: Int, val scale: Float, val offset: Float)

/**
 * One list item of the strip, in px: a whole page, or one [band] of a very tall one. A height of
 * 0 is a page of unknown size, which is laid out again once its file gives one.
 */
data class StripItem(val page: Int, val width: Int, val height: Int, val band: Band? = null)

/** The strip's items in order, and the index of each page's first item. */
class StripLayout(val items: List<StripItem>, val firstItem: List<Int>)

/**
 * Lays the pages out as list items. A page is [widthPercent] of the viewport wide, at most its own
 * width, as `pageStyle` in `useComicDisplayStore.ts`: that width is in CSS px, so it is scaled by
 * [density] here. A very tall page becomes bands of about a screen each, cut on whole px and all
 * drawn from one sampling of the page, so they meet without a seam.
 */
fun layoutStrip(
    pages: List<PageDimensions>,
    viewportWidth: Int,
    viewportHeight: Int,
    widthPercent: Int,
    density: Float,
): StripLayout {
    val items = mutableListOf<StripItem>()
    val firstItem = mutableListOf<Int>()
    val maxWidth = (viewportWidth * widthPercent / 100).coerceAtLeast(1)
    for ((index, page) in pages.withIndex()) {
        firstItem += items.size
        if (!page.sized) {
            items += StripItem(index, maxWidth, 0)
            continue
        }
        val width = min(maxWidth, (page.width * density).roundToInt())
        val height = (width.toDouble() * page.height / page.width).roundToInt().coerceAtLeast(1)
        if (height <= MAX_STRIP_PAGE_HEIGHT) {
            items += StripItem(index, width, height)
            continue
        }
        // The largest power of two that still leaves the item's width.
        val sample = Integer.highestOneBit((page.width / width).coerceAtLeast(1))
        // Source rows per px, and sampled rows per px.
        val rows = page.height.toDouble() / height
        val sampled = rows / sample
        val screen = viewportHeight.coerceAtLeast(1)
        val count = (height + screen - 1) / screen
        fun top(band: Int) = (band.toLong() * height / count).toInt()
        for (band in 0 until count) {
            val srcTop = (floor(top(band) * sampled).toInt() - BAND_OVERLAP).coerceAtLeast(0) * sample
            val srcBottom = min(page.height, (ceil(top(band + 1) * sampled).toInt() + BAND_OVERLAP) * sample)
            val offset = (srcTop / rows - top(band)).toFloat()
            items += StripItem(index, width, top(band + 1) - top(band), Band(srcTop, srcBottom, sample, (1 / sampled).toFloat(), offset))
        }
    }
    return StripLayout(items, firstItem)
}

/**
 * The page at [centre], as `updateCurrentPage` in `ReaderModeLongstrip.vue`: that of the last
 * item starting at or above it. [tops] are the offsets of the visible items, which start at item
 * [first]; an item past the pages (the end card) counts as the last page.
 */
fun StripLayout.pageAt(first: Int, tops: List<Int>, centre: Int): Int? {
    if (items.isEmpty() || tops.isEmpty()) return null
    val index = first + tops.indexOfLast { it <= centre }.coerceAtLeast(0)
    return items[index.coerceAtMost(items.lastIndex)].page
}
