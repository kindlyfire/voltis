package me.tijlvdb.voltis.data.content

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.CountDownLatch
import me.tijlvdb.voltis.data.api.testApi
import mockwebserver3.Dispatcher
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import mockwebserver3.RecordedRequest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Test

class ContentWindowTest {
    private val server = MockWebServer()
    // Written by the server's threads.
    private val requests = CopyOnWriteArrayList<Map<String, String?>>()
    @Volatile private var edition = 1
    private val held = CountDownLatch(1)
    @Volatile private var holding = emptySet<Int>()
    @Volatile private var failing = emptySet<Int>()
    @Volatile private var bucketsFail = false
    @Volatile private var bucketsGate: CountDownLatch? = null

    /** Pages of 1000 rows named by index; the page at offset 720 lacks its last 20 rows. */
    @Before
    fun start() {
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val url = request.url
                requests += url.queryParameterNames.associateWith { url.queryParameter(it) } + ("path" to url.encodedPath)
                if (url.encodedPath.endsWith("/buckets")) {
                    bucketsGate?.await()
                    if (bucketsFail) return MockResponse(code = 500)
                    return MockResponse(body = """{"total": 1000, "buckets": [{"key": "a", "count": 600}, {"key": "b", "count": 400}]}""")
                }
                val offset = url.queryParameter("offset")!!.toInt()
                if (offset in holding) held.await()
                if (offset in failing) return MockResponse(code = 500)
                val size = if (offset == 720) 100 else minOf(120, 1000 - offset)
                val rows = (offset until offset + size).joinToString(",") { """{"id": "c_$it", "title": "Tin Lantern $it v$edition", "type": "comic"}""" }
                return MockResponse(body = """{"data": [$rows]}""")
            }
        }
        server.start()
    }

    @After
    fun stop() = server.close()

    private suspend fun until(what: String, done: () -> Boolean) {
        repeat(300) {
            if (done()) return
            delay(10)
        }
        throw AssertionError("Timed out: $what")
    }

    private fun offsets() = requests.filter { it["path"]!!.endsWith("/content") }.map { it["offset"]!!.toInt() }.sorted()

    @Test(timeout = 20_000)
    fun loadsAroundTheRangeAndRefreshesInPlace() = runBlocking {
        val scope = CoroutineScope(coroutineContext + Job())
        try {
            val window = ContentWindow(scope, ContentRepository(testApi(server.url("/"))), ContentListParams(sort = "title"), start = 905)
            // The buckets and the first item's page come together; the total is the buckets'.
            until("first page") { window.itemAt(905) != null }
            until("buckets") { window.buckets != null }
            assertEquals(1000, window.buckets!!.total)
            assertEquals("Tin Lantern 905 v1", window.itemAt(905)!!.title)
            assertEquals(mapOf("sort" to "title", "limit" to "120", "offset" to "840", "count" to "false"), requests.first { it["offset"] == "840" }.minus("path"))

            // With the total known, the pages around it (one either side) follow. Index is offset: a short page leaves a gap and shifts nothing.
            until("neighbours") { window.itemAt(960) != null && window.itemAt(819) != null }
            assertEquals("Tin Lantern 840 v1", window.itemAt(840)!!.title)
            assertNull(window.itemAt(820))
            assertNotNull(window.itemAt(819))
            assertNull(window.itemAt(500))

            // A drag holds the range where it was, even a change still debouncing; the end of it applies the latest at once.
            val before = requests.size
            window.setRange(0, 10)
            window.pause(true)
            window.setRange(0, 11)
            delay(300)
            assertEquals(before, requests.size)
            window.pause(false)
            until("top pages") { window.itemAt(0) != null && window.itemAt(130) != null }
            assertEquals(listOf(0, 120, 720, 840, 960), offsets().distinct())

            // A refresh shows the old rows until the new ones land. Its requests for the old window are held while the
            // viewport moves on: the new window loads once, is kept when the refresh settles, and the held answers are dropped.
            edition = 2
            holding = setOf(0, 120)
            window.refresh()
            assertEquals("Tin Lantern 0 v1", window.itemAt(0)!!.title)
            window.setRange(600, 610)
            until("moved") { !window.refreshing }
            assertEquals("Tin Lantern 605 v2", window.itemAt(605)!!.title)
            assertEquals("Tin Lantern 725 v2", window.itemAt(725)!!.title)
            assertEquals(1, requests.count { it["offset"] == "600" })
            assertNull(window.itemAt(0))
            held.countDown()
            delay(200)
            assertNull(window.itemAt(0))

            // A refresh that fails on a cached page keeps its rows; Retry loads it again and replaces them.
            edition = 3
            failing = setOf(600)
            window.refresh()
            until("failed") { !window.refreshing }
            assertEquals("Tin Lantern 605 v2", window.itemAt(605)!!.title)
            assertNotNull(window.failure)
            failing = emptySet()
            val sent = requests.size
            window.failure!!.retry()
            until("retried") { window.itemAt(605)?.title == "Tin Lantern 605 v3" }
            assertNull(window.failure)
            assertEquals(true, requests.size > sent)

            // A page that fails and then leaves the window while the refresh is out: the final window succeeds, so no failure is left.
            failing = setOf(600)
            val gate = CountDownLatch(1)
            bucketsGate = gate
            window.refresh()
            until("page failed") { window.failure != null }
            window.setRange(0, 10)
            until("moved") { window.failure == null }
            gate.countDown()
            until("settled") { !window.refreshing && window.itemAt(0) != null }
            assertNull(window.failure)

            // A page failing before the buckets do: a range change away from the page doesn't clear it, and Retry asks for the buckets.
            window.setRange(600, 610)
            until("back") { window.itemAt(605) == null && !window.refreshing || window.itemAt(605) != null }
            val gate2 = CountDownLatch(1)
            bucketsGate = gate2
            bucketsFail = true
            window.refresh()
            until("page failed again") { window.failure != null }
            gate2.countDown()
            until("both failed") { !window.refreshing }
            window.setRange(0, 10)
            delay(300)
            assertNotNull(window.failure)
            failing = emptySet()
            bucketsFail = false
            bucketsGate = null
            val bucketRequests = requests.count { it["path"]!!.endsWith("/buckets") }
            window.failure!!.retry()
            until("recovered") { !window.refreshing && window.failure == null }
            assertEquals(bucketRequests + 1, requests.count { it["path"]!!.endsWith("/buckets") })
        } finally {
            held.countDown()
            bucketsGate?.countDown()
            scope.cancel()
        }
    }
}
