package me.tijlvdb.voltis.domain.comic

enum class ReaderMode { Paged, Longstrip }

/** Detects longstrips by the average aspect ratio of the pages with a known size. */
fun detectMode(pages: List<PageDimensions>): ReaderMode {
    val known = pages.filter { it.sized }
    if (known.isEmpty()) return ReaderMode.Paged
    val average = known.sumOf { it.height.toDouble() / it.width } / known.size
    return if (average > 1.6) ReaderMode.Longstrip else ReaderMode.Paged
}
