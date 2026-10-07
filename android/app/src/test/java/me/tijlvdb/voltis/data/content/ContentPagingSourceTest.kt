package me.tijlvdb.voltis.data.content

import androidx.paging.PagingConfig
import androidx.paging.PagingSource
import androidx.paging.PagingSource.LoadResult
import androidx.paging.testing.TestPager
import kotlinx.coroutines.runBlocking
import me.tijlvdb.voltis.data.api.testApi
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class ContentPagingSourceTest {
    private val server = MockWebServer()

    @Before
    fun start() = server.start()

    @After
    fun stop() = server.close()

    private fun page(count: Int, total: Int? = null): MockResponse {
        val rows = (1..count).joinToString(",") { """{"id": "c_$it", "title": "Tin Lantern $it", "type": "comic"}""" }
        return MockResponse(body = """{"data": [$rows], "total": $total}""")
    }

    private fun source(totals: MutableList<Int> = mutableListOf(), reach: PagingReach = PagingReach()) =
        ContentPagingSource(ContentRepository(testApi(server.url("/"))), ContentListParams(sort = "continue"), reach, onTotal = totals::add)

    private fun pager(source: ContentPagingSource = source()) = TestPager(PagingConfig(ContentPagingSource.PAGE_SIZE), source)

    /** The query of the next request the server got. */
    private fun query() = server.takeRequest().url.let { url -> url.queryParameterNames.associateWith { url.queryParameter(it) } }

    private fun LoadResult<Int, PagedContent>?.nextKey() = (this as LoadResult.Page).nextKey

    @Test(timeout = 10_000)
    fun pagesByOffsetToTheEnd() = runBlocking {
        val totals = mutableListOf<Int>()
        val reach = PagingReach()
        val pager = pager(source(totals, reach))

        // The first page asks for the total. It is short, and the next offset still follows the limit.
        server.enqueue(page(2, total = 130))
        assertEquals(60, pager.refresh().nextKey())
        assertEquals(mapOf("sort" to "continue", "limit" to "60", "offset" to "0", "count" to "true"), query())
        assertEquals(listOf(130), totals)

        server.enqueue(page(3))
        val second = pager.append() as LoadResult.Page
        assertEquals(120, second.nextKey)
        // Rows carry their offsets, which key them; the page before is one size back, and the first has none.
        assertEquals(listOf(60, 61, 62), second.data.map { it.offset })
        assertEquals(0, second.prevKey)
        assertEquals(mapOf("sort" to "continue", "limit" to "60", "offset" to "60", "count" to "false"), query())

        // Paging asks the next generation's source for the refresh key. It reloads to the end of
        // the anchored page by its offsets: row 4 of 5 sits in the page that ends at 120,
        // whatever its position says.
        assertEquals(120, source(reach = reach).getRefreshKey(pager.getPagingState(4)))

        // 120 + 60 reaches the total: the end, without an extra request. The last page has no
        // next key, and still refreshes to its end.
        server.enqueue(page(9))
        assertEquals(null, pager.append().nextKey())
        assertEquals("120", query()["offset"])
        assertEquals(14, pager.getPages().sumOf { it.data.size })
        assertEquals(listOf(130), totals)
        assertEquals(180, source(reach = reach).getRefreshKey(pager.getPagingState(13)))

        // An empty page ends the list, whatever the total says.
        val other = pager()
        server.enqueue(page(60, total = 500))
        other.refresh()
        server.enqueue(page(0))
        assertEquals(null, other.append().nextKey())
        repeat(2) { server.takeRequest() }

        // A refresh loads as far as its key says, in one request from the start. When that one
        // short page is also the end, the next refresh still reaches as far.
        val combined = PagingReach()
        val scrolled = pager(source(reach = combined))
        server.enqueue(page(14, total = 130))
        assertEquals(null, scrolled.refresh(initialKey = 180).nextKey())
        assertEquals("180" to "0", query().let { it["limit"] to it["offset"] })
        val successor = source(reach = combined)
        val again = successor.getRefreshKey(scrolled.getPagingState(13))
        assertEquals(180, again)
        server.enqueue(page(14, total = 130))
        pager(successor).refresh(initialKey = again)
        assertEquals("180", query()["limit"])
    }

    @Test(timeout = 10_000)
    fun aDroppedPageLoadsAgainByItsOffset() = runBlocking {
        val reach = PagingReach().apply { offset = 180 }
        val source = source(reach = reach)
        suspend fun prepend() = source.load(PagingSource.LoadParams.Prepend(120, 60, false))

        // The page before the retained rows fails, then loads on retry, in front of them.
        server.enqueue(MockResponse(code = 500))
        assertTrue(prepend() is LoadResult.Error)
        server.takeRequest()
        server.enqueue(page(60))
        val again = prepend() as LoadResult.Page
        assertEquals("120", query()["offset"])
        assertEquals((120..179).toList(), again.data.map { it.offset })
        assertEquals(60, again.prevKey)
        // Nothing before the window says how far the list was loaded.
        assertEquals(180, reach.offset)
    }
}
