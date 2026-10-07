package me.tijlvdb.voltis.domain.comic

/** Page indices in preload order: the current page, then forward, with at most two pages back. */
fun getPagesInPreloadOrder(pageCount: Int, currentPage: Int): List<Int> = buildList {
    for (i in 0 until pageCount) {
        val forward = currentPage + i
        val backward = currentPage - i
        if (forward < pageCount) add(forward)
        if (i <= 2 && backward != forward && backward >= 0) add(backward)
    }
}
