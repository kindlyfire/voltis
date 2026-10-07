package me.tijlvdb.voltis.data.downloads

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentLength
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.FileData
import me.tijlvdb.voltis.data.api.testApi
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.db.importReading
import me.tijlvdb.voltis.domain.reading.ReadingSnapshot
import me.tijlvdb.voltis.domain.reading.ReadingState
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.RequestedBy
import me.tijlvdb.voltis.domain.downloads.SeriesPolicy
import mockwebserver3.Dispatcher
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import mockwebserver3.RecordedRequest
import okhttp3.Headers.Companion.headersOf
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** `enqueue` scales (P2 Addendum 5): requests per series, not per volume. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [37], application = Application::class)
class EnqueueTest {
    @get:Rule
    val temp = TemporaryFolder()

    private val server = MockWebServer()
    private val db = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), VoltisDatabase::class.java).build()
    private val scope = CoroutineScope(SupervisorJob())
    private lateinit var store: DownloadStore
    private val requests = mutableListOf<String>()

    private fun volumes(series: String) = List(125) { Content("$series-$it", "Volume $it", ContentType.COMIC, parentId = series) }

    @Before
    fun start() {
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val url = request.url
                synchronized(requests) { requests += url.encodedPath + (url.query?.let { "?$it" } ?: "") }
                // A gateway with the server down.
                if (url.encodedPath.endsWith("/away")) return MockResponse(502, headersOf("Content-Type", "text/html"), "<html>")
                val body = if (url.encodedPath == "/api/content") {
                    """{"data": ${AppJson.encodeToString(ListSerializer(Content.serializer()), volumes(url.queryParameter("parent_id")!!))}}"""
                } else {
                    val id = url.pathSegments.last()
                    AppJson.encodeToString(Content.serializer(), Content(id, "Series $id", ContentType.COMIC_SERIES))
                }
                return MockResponse(200, headersOf("Content-Type", "application/json"), body)
            }
        }
        server.start()
        store = DownloadStore(AccountStore("a", temp.root, db), scope, {})
    }

    @After
    fun stop() = runBlocking {
        store.stop()
        scope.cancel()
        server.close()
        db.close()
    }

    @Test
    fun queuesManyVolumesWithFourRequests() = runBlocking {
        val seeded = mutableListOf<Int>()
        var started = 0
        val items = volumes("s1") + volumes("s2")
        enqueueDownloads(
            store, testApi(server.url("/")), items, RequestedBy.USER,
            seed = { rows -> seeded += rows.size }, now = { 5 }, schedule = { started++ },
        )
        assertEquals(
            listOf("s1", "s2").flatMap { listOf("/api/content/$it", "/api/content?parent_id=$it&sort=order&sort_order=asc&count=false") },
            requests,
        )
        assertEquals(listOf(126, 126), seeded)
        // Once, after the last chunk.
        assertEquals(1, started)
        assertEquals(250, db.downloads().all().size)
        assertEquals("s1-0" to DownloadState.QUEUED, db.downloads().next()!!.let { it.contentId to it.state })
        assertEquals(124, db.content().get("s2-124")!!.position)

        // A series that can't be fetched: queued from the row as passed.
        requests.clear()
        enqueueDownloads(
            store, testApi(server.url("/")), listOf(Content("v-away", "Volume", ContentType.COMIC, parentId = "away")), RequestedBy.USER,
            seed = { rows -> seeded += rows.size }, now = { 5 }, schedule = { started++ },
        )
        assertEquals(listOf("/api/content/away"), requests)
        assertEquals(DownloadState.QUEUED, db.downloads().get("v-away")?.state)
        assertEquals("Volume", db.content().get("v-away")?.title)
        assertEquals(listOf(126, 126, 1), seeded)

        // Any ID is queued; a standalone detail row is cached whole, but seeds the lane without its pages.
        val pages = FileData(List(3) { JsonArray(listOf(JsonPrimitive("$it.png"))) })
        val detail = Content("odd/id ?", "Lantern Isle", ContentType.COMIC, fileData = pages, length = ContentLength("pages", 3, 3))
        val seeds = mutableListOf<Content>()
        enqueueDownloads(store, testApi(server.url("/")), listOf(detail), RequestedBy.USER, seed = { rows -> seeds += rows }, now = { 5 }, schedule = {})
        assertEquals(DownloadState.QUEUED, db.downloads().get("odd/id ?")?.state)
        assertEquals(listOf(detail.copy(fileData = FileData(), length = null)), seeds)
        assertEquals(detail, AppJson.decodeFromString(Content.serializer(), db.content().get("odd/id ?")!!.json))
    }

    /** A failed submission keeps every chunk's rows and reaches the caller once; a later `ensureScheduled` makes it up. */
    @Test
    fun aFailedScheduleLeavesEveryChunkQueuedAndIsMadeUpLater() = runBlocking {
        var schedules = 0
        val failure = runCatching {
            enqueueDownloads(
                store, testApi(server.url("/")), volumes("s1"), RequestedBy.USER, seed = { _ -> }, now = { 5 },
                schedule = {
                    schedules++
                    throw QueueSubmitFailed(IllegalStateException("disk"))
                },
            )
        }.exceptionOrNull()
        assertTrue(failure is QueueSubmitFailed)
        assertEquals(1, schedules)
        assertEquals(125, db.downloads().all().size)

        // A bulk call over the same rows queues nothing new, and still schedules them.
        var bulkStarts = 0
        val bulk = BulkQueue({ store }, { _, _ -> }, { bulkStarts++ })
        assertEquals(BulkQueued(0, false), bulk.queue(store, volumes("s1")))
        assertEquals(1, bulkStarts)

        val submitted = mutableListOf<Boolean>()
        val starter = QueueStarter({ store }, { false }, { false }, { _, _, append -> submitted += append }, {})
        starter.ensureScheduled(store)
        assertEquals(listOf(false), submitted)
        // A wake is submitted once, by whoever gets there first; a replacement is submitted all the same.
        val wake = store.wakes.value
        starter.ensureScheduled(store, wake = wake)
        starter.ensureScheduled(store, wake = wake)
        assertEquals(listOf(false, false), submitted)
        starter.ensureScheduled(store, replace = true, wake = wake)
        assertEquals(listOf(false, false, false), submitted)
        // Nothing runnable: nothing is submitted.
        store.delete(db.downloads().all().map { it.contentId })
        starter.ensureScheduled(store)
        assertEquals(3, submitted.size)

        // The reads before the submission fail the same way.
        val unreadable = QueueStarter({ store }, { throw IllegalStateException("settings") }, { false }, { _, _, _ -> }, {})
        assertTrue(runCatching { unreadable.start() }.exceptionOrNull() is QueueSubmitFailed)
    }

    /** Seeding held after the cache commit, the series' last download cancelled: the reserved cache survives, and the save queues its window at once. */
    @Test
    fun aReservedBootstrapSurvivesTheLastDownloadsCancel() = runBlocking {
        store.enqueue(listOf(NewDownload("s1-gone", "s1", RequestedBy.USER)))
        val held = store.reserve(listOf("s1"))
        val seeding = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
        try {
            val cached = async {
                cacheSeries(store, testApi(server.url("/")), "s1", { _ -> seeding.complete(Unit); release.await() }, now = { System.currentTimeMillis() })
            }
            seeding.await()
            store.cancel("s1-gone")
            assertTrue(db.downloads().all().isEmpty())
            release.complete(Unit)
            cached.await()
            assertEquals(AutoApplied(2, 0), store.setPolicy("s1", SeriesPolicy("s1", 2, false)))
        } finally {
            held.release()
        }
        // Released: the next removal prunes as usual.
        store.setPolicy("s1", null)
        assertNull(db.content().get("s1-0"))
    }

    /** A policy's first save caches the series and its whole list and seeds every volume, so the window is queued at once. */
    @Test
    fun aPolicyBootstrapsASeriesThatWasNeverDownloaded() = runBlocking {
        val api = testApi(server.url("/"))
        val seeded = mutableListOf<Int>()
        suspend fun save(id: String) {
            cacheSeries(store, api, id, { rows -> seeded += rows.size }, now = { 5 })
            store.setPolicy(id, SeriesPolicy(id, 2, false))
        }

        // A failed fetch saves nothing.
        assertTrue(runCatching { save("away") }.isFailure)
        assertNull(db.auto().policy("away"))
        assertNull(db.content().get("away"))

        // An older cache holds a completed, downloaded volume the server no longer lists: it must not anchor the window.
        db.cache(listOf(Content("s1-gone", "Gone", ContentType.COMIC, parentId = "s1") to 200), 1) { false }
        db.importReading(listOf(ReadingSnapshot("s1-gone", ReadingState("r", "completed", seq = 1))))
        db.downloads().insert(listOf(DownloadEntity("s1-gone", "s1", DownloadState.DONE, requestedBy = RequestedBy.USER, queuedAt = 1, copyId = "c", copyVersion = "v", copyPageCount = 1, copyBytes = 1)))

        save("s1")
        assertEquals(listOf(126), seeded)
        assertEquals(125, db.content().volumes("s1").size)
        assertEquals(false, db.content().get("s1-gone")!!.valid)
        assertEquals(
            mapOf("s1-gone" to RequestedBy.USER, "s1-0" to RequestedBy.AUTO, "s1-1" to RequestedBy.AUTO),
            db.downloads().all().associate { it.contentId to it.requestedBy },
        )

        // Removed: the queued rows and the cached rows go. Set again: the window is queued again.
        store.delete(listOf("s1-gone"))
        store.setPolicy("s1", null)
        assertTrue(db.downloads().all().isEmpty())
        assertNull(db.content().get("s1-0"))
        save("s1")
        assertEquals(2, db.downloads().all().size)
    }
}
