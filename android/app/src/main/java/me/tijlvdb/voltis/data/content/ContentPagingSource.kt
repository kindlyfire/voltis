package me.tijlvdb.voltis.data.content

import androidx.paging.PagingSource
import androidx.paging.PagingState
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.attemptResult

/**
 * How far one list has been loaded, shared by the successive sources of one Pager: Paging asks
 * the next generation's source for the refresh key, which has loaded nothing itself.
 */
class PagingReach {
    /** The offset the last page reached: the list's end has no next key to say it. */
    var offset = 0
}

/** A listed row with its offset in the list: a key that stays the same when pages ahead of it are dropped, which a row's ID does not (a list that changed can repeat one). */
class PagedContent(val content: Content, val offset: Int)

/**
 * A `GET /content` list: the key is the offset, and the pages before the first are loaded again by
 * theirs, as a bounded window of pages drops the ones it scrolled away from. Each load waits for [ready] (the
 * start's connectivity check). [onTotal] gets the total, which only the first page asks for.
 */
class ContentPagingSource(
    private val content: ContentRepository,
    private val query: ContentListParams,
    private val reach: PagingReach,
    private val ready: suspend () -> Unit = {},
    private val onTotal: (Int) -> Unit = {},
) : PagingSource<Int, PagedContent>() {
    private var total: Int? = null

    override suspend fun load(params: LoadParams<Int>): LoadResult<Int, PagedContent> {
        // A refresh starts over; its key says how far to load, so a scrolled list stays in place.
        ready()
        val refresh = params is LoadParams.Refresh
        val prepend = params is LoadParams.Prepend
        val offset = if (refresh) 0 else params.key ?: 0
        val limit = if (refresh) maxOf(PAGE_SIZE, params.key ?: 0) else PAGE_SIZE
        val page = attemptResult { content.list(query.copy(limit = limit, offset = offset, count = offset == 0)) }
            .getOrElse { return LoadResult.Error(it) }
        page.total?.let {
            total = it
            onTotal(it)
        }
        // Not `offset + data.size`: in the continue sorts the server drops rows after its
        // limited query, so a page can be short in the middle of the list.
        val next = offset + limit
        // A page before the window says nothing of how far the list was loaded.
        if (!prepend) reach.offset = next
        val ended = page.data.isEmpty() || total?.let { next >= it } == true
        // Pages start on multiples of the page size (a refresh covers several), so the one before is one size back.
        val before = if (offset > 0) maxOf(0, offset - PAGE_SIZE) else null
        return LoadResult.Page(page.data.mapIndexed { i, row -> PagedContent(row, offset + i) }, prevKey = before, nextKey = if (ended) null else next)
    }

    /**
     * The offset the anchored page ends at, from the pages' keys: after short pages the item's
     * position says less than the offsets consumed. Capped, so a list scrolled deeper is clamped.
     */
    override fun getRefreshKey(state: PagingState<Int, PagedContent>): Int? {
        val page = state.closestPageToPosition(state.anchorPosition ?: return null) ?: return null
        return (page.nextKey ?: reach.offset).coerceAtMost(MAX_REFRESH)
    }

    companion object {
        const val PAGE_SIZE = 60
        private const val MAX_REFRESH = 600

        /** The most rows a grid keeps loaded: a whole refresh, which is capped at [MAX_REFRESH]. */
        const val MAX_LOADED = MAX_REFRESH
    }
}
