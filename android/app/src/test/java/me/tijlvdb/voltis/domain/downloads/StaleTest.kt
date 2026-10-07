package me.tijlvdb.voltis.domain.downloads

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentLength
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.DisplayMetadata
import me.tijlvdb.voltis.data.api.FileData
import me.tijlvdb.voltis.data.api.testApi
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.data.db.LaneEntity
import me.tijlvdb.voltis.data.db.OpEntity
import me.tijlvdb.voltis.data.db.SeriesPolicyEntity
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.downloads.CopyMade
import me.tijlvdb.voltis.data.downloads.DownloadStore
import me.tijlvdb.voltis.data.downloads.NewDownload
import me.tijlvdb.voltis.data.downloads.Observation
import me.tijlvdb.voltis.data.downloads.OfflineManifest
import me.tijlvdb.voltis.data.downloads.OfflinePage
import me.tijlvdb.voltis.data.downloads.Run
import me.tijlvdb.voltis.data.downloads.cache
import me.tijlvdb.voltis.data.downloads.refreshCatalog
import me.tijlvdb.voltis.domain.reading.OpKind
import mockwebserver3.Dispatcher
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import mockwebserver3.RecordedRequest
import okhttp3.Headers.Companion.headersOf
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [37], application = Application::class)
class StaleTest {
    @get:Rule
    val temp = TemporaryFolder()

    private val server = MockWebServer()
    private val db = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), VoltisDatabase::class.java).build()
    private val scope = CoroutineScope(SupervisorJob())
    private var now = 5L

    /** The refresher's clock. */
    @Volatile private var time = 5L
    private val store by lazy { DownloadStore(AccountStore("a", temp.root, db), scope, {}, clock = { now }) }

    @After
    fun close() = runBlocking {
        store.stop()
        scope.cancel()
        server.close()
        db.close()
    }

    private fun volume(id: String, mtime: String, size: Long, title: String = id) =
        Content(id, title, ContentType.COMIC, parentId = "s_one", fileMtime = mtime, fileSize = size)

    /** A downloaded copy of the file at [mtime] and [size]. */
    private fun done(id: String, series: String, mtime: String, size: Long, stale: String? = null) = DownloadEntity(
        id, series, DownloadState.DONE, requestedBy = RequestedBy.USER, queuedAt = 1,
        copyId = "copy-$id", copyVersion = "v", copyPageCount = 1, copyFileMtime = mtime, copyFileSize = size, stale = stale,
    )

    private fun stale(id: String) = runBlocking { db.downloads().get(id)?.stale }

    @Test
    fun refreshUpdatesRowsAndMarksStale() = runBlocking {
        val t = "2026-01-10T06:00:00Z"
        val list = listOf(volume("v_same", "2026-01-10T10:00:00+04:00", 10, "Renamed"), volume("v_new", t, 99), volume("v_other", t, 5))
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val json = headersOf("Content-Type", "application/json")
                fun rows(rows: List<Content>) = MockResponse(200, json, """{"data": ${AppJson.encodeToString(ListSerializer(Content.serializer()), rows)}}""")
                fun row(content: Content) = MockResponse(200, json, AppJson.encodeToString(Content.serializer(), content))
                return when (request.url.encodedPath) {
                    "/api/content" -> when (request.url.queryParameter("parent_id")) {
                        "s_one" -> rows(list)
                        "s_policy" -> rows(listOf(volume("p1", t, 3).copy(parentId = "s_policy"), volume("p2", t, 4).copy(parentId = "s_policy")))
                        else -> {
                            // Delete all runs while this answer is on its way.
                            runBlocking { store.delete(listOf("v_three")) }
                            rows(listOf(volume("v_three", t, 1).copy(parentId = "s_three")))
                        }
                    }
                    "/api/content/s_one" -> {
                        // Gone, as a request saw it after the series' request started and before its list's.
                        if (time == 10L) {
                            runBlocking { store.markGone("v_same", at = 20) }
                            time = 30
                        }
                        row(Content("s_one", "Series", ContentType.COMIC_SERIES))
                    }
                    "/api/content/s_policy" -> row(Content("s_policy", "Policy", ContentType.COMIC_SERIES))
                    "/api/content/s_three" -> row(Content("s_three", "Third", ContentType.COMIC_SERIES))
                    "/api/content/c_alone" -> row(Content("c_alone", "Alone", ContentType.COMIC, valid = false, fileData = FileData(listOf(JsonArray(listOf(JsonPrimitive("0.png"))))), length = ContentLength("pages", 1, 1)))
                    // Deleted (c_deleted, and the series s_two): Voltis says so.
                    else -> MockResponse(404, json, """{"error": "Content not found"}""")
                }
            }
        }
        server.start()
        val api = testApi(server.url("/"))
        val seeded = mutableListOf<List<String>>()
        val seeds = mutableListOf<Content>()
        suspend fun refresh() = refreshCatalog(
            store, api,
            { rows ->
                seeded += rows.map { it.id }
                seeds += rows
            },
            now = { time },
        )

        // Nothing downloaded: nothing reached, so an automatic pass isn't held back by it.
        assertFalse(refresh())

        db.cache(listOf(volume("v_same", t, 10).copy(meta = DisplayMetadata(description = "Detail")) to 0), 1) { true }
        db.cache(listOf(volume("v_gone", t, 7) to 3, volume("v_unneeded", t, 7) to 4, volume("v_unsent", t, 7) to 5), 1) { false }
        db.downloads().insert(
            listOf(
                done("v_same", "s_one", t, 10, stale = Stale.VERSION), done("v_new", "s_one", t, 10), done("v_gone", "s_one", t, 7),
                done("c_alone", "c_alone", t, 1), done("c_deleted", "c_deleted", t, 1), done("v_two", "s_two", t, 1), done("v_three", "s_three", t, 1),
                // Queued, without a copy: the observation is only stored.
                DownloadEntity("v_other", "s_one", DownloadState.QUEUED, requestedBy = RequestedBy.USER, queuedAt = 1, transferId = "t-other"),
            ),
        )
        // A series with a policy and no download is fetched too.
        db.auto().upsertPolicy(SeriesPolicyEntity("s_policy", 2, false, 1))
        // Reading of v_unsent that hasn't synced keeps its row.
        db.lanes().upsert(listOf(lane("v_unsent", "s_one")))
        db.ops().insert(listOf(OpEntity(0, "v_unsent", OpKind.POSITION, "{}", false, null, null, null, null, null, 0, null, 0)))
        assertTrue(refresh())

        val rows = db.downloads().all().associateBy { it.contentId }
        assertEquals(
            mapOf(
                "v_same" to null, "v_new" to Stale.VERSION, "v_gone" to Stale.GONE, "c_alone" to Stale.GONE, "c_deleted" to Stale.GONE,
                "v_two" to Stale.GONE, "v_other" to null,
            ),
            rows.mapValues { it.value.stale },
        )
        assertEquals(listOf<Any?>(5L, false, t, 5L), rows.getValue("v_other").let { listOf(it.seenAt, it.seenGone, it.seenMtime, it.seenSize) })
        // A list row updates a detail row's columns, not its json.
        val same = db.content().get("v_same")!!
        assertEquals("Renamed" to 0, same.title to same.position)
        assertEquals("Detail", AppJson.decodeFromString(Content.serializer(), same.json).meta.description)
        // Gone from the list: kept, invalid, while downloaded or with unsent reading; else deleted.
        assertEquals(listOf(false, false), listOf("v_gone", "v_unsent").map { db.content().get(it)!!.valid })
        assertEquals(listOf("v_new", "v_other", "v_same"), db.content().volumes("s_one").map { it.id }.sorted())
        assertNull(db.content().get("v_unneeded"))
        // Deleted before its answer landed: nothing cached, nothing seeded.
        assertNull(db.content().get("s_three"))
        assertNull(db.content().get("v_three"))
        // The series and its downloaded volumes, with when their requests started.
        assertEquals(listOf("s_one", "v_same", "v_new", "v_other"), seeded.first())
        assertTrue(seeded.none { "s_three" in it })
        // The policy series is cached, not pruned, and every listed volume is seeded.
        assertEquals(listOf("p1", "p2"), db.content().volumes("s_policy").map { it.id })
        assertEquals(listOf("s_policy", "p1", "p2"), seeded.last())
        store.setPolicy("s_policy", null)
        assertNull(db.content().get("p1"))
        // A detail row seeds its lane without its pages.
        assertEquals(listOf<Any?>(FileData(), null), seeds.first { it.id == "c_alone" }.let { listOf(it.fileData, it.length) })

        // Each list is observed as of its own request: a Gone seen between the series' request and its list's is older than the list.
        time = 10
        assertTrue(refresh())
        assertEquals(listOf<Any?>(null, 30L, false), db.downloads().get("v_same")!!.let { listOf(it.stale, it.seenAt, it.seenGone) })
    }

    @Test
    fun aSeriesAnswerCountsWhenItsListDoesnt() = runBlocking {
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse = when (request.url.encodedPath) {
                "/api/content/s_four" -> MockResponse(200, headersOf("Content-Type", "application/json"), AppJson.encodeToString(Content.serializer(), Content("s_four", "Four", ContentType.COMIC_SERIES)))
                // A gateway with the server gone meanwhile.
                else -> MockResponse(502, headersOf("Content-Type", "text/html"), "<html>")
            }
        }
        server.start()
        val t = "2026-01-10T06:00:00Z"
        db.cache(listOf(volume("v_four", t, 1).copy(parentId = "s_four") to 0), 1) { false }
        db.downloads().insert(listOf(done("v_four", "s_four", t, 1)))
        val before = db.content().get("v_four")
        assertTrue(refreshCatalog(store, testApi(server.url("/")), { _ -> }, now = { 5 }))
        // The cache needs the whole pair: nothing written, nothing marked.
        assertEquals(before, db.content().get("v_four"))
        assertNull(db.content().get("s_four"))
        assertNull(db.downloads().get("v_four")!!.stale)
    }

    private fun manifest(id: String, mtime: String) = OfflineManifest(1, id, "v-$mtime", fileSize = 10, fileMtime = mtime, pageCount = 1, from = 0, pages = listOf(OfflinePage("0.png")))

    /** A whole transfer of the file at [mtime], whose stream started at [at]; [meanwhile] runs between its manifest and its publication. */
    private suspend fun download(mtime: String, at: Long, meanwhile: suspend (Run) -> Unit = {}) = store.runNext { run ->
        assertNotNull(store.manifest(run, manifest(run.contentId, mtime), at))
        meanwhile(run)
        store.page(run, 0, 10)
        store.publish(run, CopyMade(10, cover = false, seriesCover = false))
    }!!.result!!

    @Test
    fun theNewestObservationDecides() = runBlocking {
        val a = "2026-01-10T06:00:00Z"
        val b = "2026-02-10T06:00:00Z"
        fun seen(mtime: String, at: Long) = Observation("lantern-1", ServerFile(true, mtime, 10), at)
        store.enqueue(listOf(NewDownload("lantern-1", "lantern", RequestedBy.USER)))

        // The file changed while the first copy was downloading, after its stream started: it publishes as stale.
        assertTrue(download(a, at = 10) { store.observe(listOf(seen(b, 20))) })
        assertEquals(Stale.VERSION, stale("lantern-1"))

        // A delayed observation older than a Gone is ignored.
        store.markGone("lantern-1", at = 30)
        store.observe(listOf(seen(a, 25)))
        assertEquals(Stale.GONE, stale("lantern-1"))

        // Back, changed; a Gone held from before that observation is rejected whole: the mark and the running replacement survive.
        store.observe(listOf(seen(b, 40)))
        store.again("lantern-1")
        assertTrue(
            download(b, at = 50) { run ->
                store.markGone("lantern-1", at = 35)
                val row = db.downloads().get("lantern-1")!!
                assertEquals(listOf<Any?>(DownloadState.RUNNING, run.dir.name, Stale.VERSION), listOf(row.state, row.transferId, row.stale))
                // An observation matching the old copy, made after the replacement's stream started.
                store.observe(listOf(seen(a, 60)))
            },
        )
        // The replacement publishes, stale against that newer observation.
        assertEquals(b to Stale.VERSION, db.downloads().get("lantern-1")!!.let { it.copyFileMtime to it.stale })
    }

    private fun lane(id: String, parent: String) = LaneEntity(
        id, null, null, parent, ContentType.COMIC, id, null, true, false, null, null, null, "{}", null, null, 0, 0, null, null,
        true, false, 0, false, null, "{}", null, 0,
    )
}
