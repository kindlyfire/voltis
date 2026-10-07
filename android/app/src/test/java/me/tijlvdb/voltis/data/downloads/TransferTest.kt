package me.tijlvdb.voltis.data.downloads

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.File
import java.util.Base64
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.downloads.Transfer.Outcome
import me.tijlvdb.voltis.domain.downloads.DownloadError
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.ErrorKind
import me.tijlvdb.voltis.domain.downloads.RequestedBy
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import okhttp3.Headers.Companion.headersOf
import okhttp3.OkHttpClient
import okio.Buffer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** P2 §8, One item: a claimed run against MockWebServer, in a temporary account directory. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [37], application = Application::class)
class TransferTest {
    @get:Rule
    val temp = TemporaryFolder()

    private val server = MockWebServer()
    private val db = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), VoltisDatabase::class.java).build()
    private val scope = CoroutineScope(SupervisorJob())
    private lateinit var store: DownloadStore
    private val counts = mutableListOf<Pair<String, Int>>()
    private var free = Long.MAX_VALUE
    private var now = 1_000L

    /** Thrown by the free-space check of this item: a bug in the transfer. */
    private var broken: String? = null

    /** Runs in the next free-space check: the last step before a request. */
    private var checking: (() -> Unit)? = null

    @Before
    fun start() {
        server.start()
        lateinit var owner: DownloadStore
        owner = DownloadStore(AccountStore("a", temp.root, db), scope, { owner.collectSoon(it.dirId) }, clock = { now })
        store = owner
    }

    @After
    fun stop() = runBlocking {
        store.stop()
        scope.cancel()
        server.close()
        db.close()
    }

    /** Claims the queue's head and transfers it. */
    private fun run(): Outcome? = runBlocking {
        store.runNext { claimed ->
            Transfer(
                store, OkHttpClient(), server.url("/"),
                pageCount = { _, id, n -> counts += id to n },
                freeBytes = {
                    checking?.also { checking = null }?.invoke()
                    if (claimed.contentId == broken) error("Free space unknown") else free
                },
                clock = { now },
            ).run(claimed)
        }?.result
    }

    private fun row(id: String) = runBlocking { db.downloads().get(id)!! }

    /** Queued with its series and detail rows, so the transfer asks for neither. */
    private fun queue(vararg ids: String) = runBlocking {
        db.cache(listOf(Content("s", "Lantern Isle", ContentType.COMIC_SERIES) to null) + ids.map { Content(it, "Lantern Isle $it", ContentType.COMIC, parentId = "s") to 0 }, now) { true }
        store.enqueue(ids.map { NewDownload(it, "s", RequestedBy.USER) })
    }

    private fun json(content: Content) = MockResponse(200, headersOf("Content-Type", "application/json"), AppJson.encodeToString(Content.serializer(), content))

    /** A transparent 30x90 PNG: valid image bytes, with no graphics stack to make them. */
    private fun png(): ByteArray = Base64.getDecoder().decode("iVBORw0KGgoAAAANSUhEUgAAAB4AAABaCAYAAACv+ebYAAAAIUlEQVR42u3BMQEAAADCoPVPbQdvoAAAAAAAAAAAAADgMSqKAAFo7agbAAAAAElFTkSuQmCC")

    /** A comic of 3 pages; pages `from until until`, then the end, an error frame, or nothing. Unsized pages are [page]'s bytes. */
    private fun stream(
        from: Int = 0,
        until: Int = 3,
        version: String = "v1",
        end: Boolean = true,
        error: String? = null,
        fileSize: Long? = 120,
        page: ((Int) -> ByteArray)? = null,
    ) = Buffer().apply {
        val pages = List(3) { if (page == null) """{"name": "p$it.jpg", "width": 10, "height": 15}""" else """{"name": "p$it.png"}""" }
        val json = """{"format": 1, "content_id": "c", "version": "$version", "file_size": $fileSize, "file_mtime": "2026-10-04T15:12:00Z",
            "page_count": 3, "from": $from, "pages": [${pages.joinToString()}]}"""
        writeInt(json.length)
        writeUtf8(json)
        for (i in from until until) {
            val bytes = page?.invoke(i) ?: ByteArray(4) { i.toByte() }
            writeInt(i)
            writeInt(bytes.size)
            write(bytes)
        }
        if (error != null) {
            writeInt(0xFFFFFFFE.toInt())
            writeInt(error.length)
            writeUtf8(error)
        } else if (end) {
            writeInt(-1)
            writeInt(0)
        }
    }

    private fun pages(body: Buffer) = MockResponse.Builder().code(200).addHeader("Content-Type", "application/vnd.voltis.pages").body(body).build()

    private fun voltis(code: Int, message: String) = MockResponse(code, headersOf("Content-Type", "application/json"), """{"error": "$message"}""")

    private fun image(cache: String) = MockResponse.Builder().code(200).addHeader("Content-Type", "image/jpeg").addHeader("Cache-Control", cache).body("jpeg").build()

    private fun target() = server.takeRequest().url.let { it.encodedPath + (it.query?.let { q -> "?$q" } ?: "") }

    private fun dir(name: String?) = File(temp.root, "downloads/$name")

    @Test
    fun downloadsAndResumes() {
        // Only a list row and no series: both detail rows are fetched first.
        runBlocking {
            db.cache(listOf(Content("c1", "Lantern Isle Vol. 1", ContentType.COMIC, parentId = "s", coverVersion = "cv") to 0), now) { false }
            store.enqueue(listOf(NewDownload("c1", "s", RequestedBy.USER)))
        }
        server.enqueue(json(Content("c1", "Lantern Isle Vol. 1", ContentType.COMIC, parentId = "s", coverVersion = "cv")))
        server.enqueue(json(Content("s", "Lantern Isle", ContentType.COMIC_SERIES, coverVersion = "sv")))
        val png = List(3) { png() }
        // Cut off after the first page, unsized; a kill then leaves half of the second.
        server.enqueue(pages(stream(until = 1, end = false, page = png::get)))
        assertEquals(Outcome.RETRY, run())
        assertEquals(listOf("/api/content/c1", "/api/content/s", "/api/files/offline/c1?from=0"), listOf(target(), target(), target()))
        val transfer = dir(row("c1").transferId)
        assertEquals(listOf<Any?>(DownloadState.QUEUED, 1, 0, DownloadError.UNREACHABLE), row("c1").let { listOf(it.state, it.pagesDone, it.attempts, it.error) })
        File(transfer, "1.part").writeText("half")

        // The next run resumes from page 1, then stores the covers, the series' first, except a fallback.
        server.enqueue(pages(stream(from = 1, page = png::get)))
        server.enqueue(image("no-store"))
        server.enqueue(image("public, max-age=31536000, immutable"))
        assertEquals(Outcome.NEXT, run())
        assertEquals(listOf("/api/files/offline/c1?from=1", "/api/files/cover/s?v=sv", "/api/files/cover/c1?v=cv"), listOf(target(), target(), target()))
        val copy = row("c1")
        assertEquals(listOf<Any?>(DownloadState.DONE, transfer.name, null, true, false), listOf(copy.state, copy.copyId, copy.transferId, copy.copyCover, copy.copySeriesCover))
        assertEquals(listOf("0", "1", "2", COVER, MANIFEST), transfer.list()!!.sorted())
        assertEquals(png.map { it.toList() }, (0..2).map { File(transfer, "$it").readBytes().toList() })
        // The sizes the server didn't have, from the pages, in the copy's own manifest.
        assertEquals(List(3) { 30 to 90 }, manifestIn(transfer)!!.pages.map { it.width to it.height })
        assertEquals(transfer.listFiles()!!.sumOf { it.length() }, copy.copyBytes)
        assertEquals(listOf("c1" to 3), counts)
    }

    @Test
    fun restartsFromTheStartInANewDirectory() {
        // A new version on resuming, a 400 for `from > 0`, and a stored manifest of another version: from page 0, once, elsewhere.
        val firsts = listOf(pages(stream(from = 2, version = "v2")), voltis(400, "Invalid from"), null)
        for ((i, first) in firsts.withIndex()) {
            val id = "r$i"
            queue(id)
            server.enqueue(pages(stream(until = 2, end = false)))
            assertEquals(Outcome.RETRY, run())
            target()
            val old = dir(row(id).transferId)
            if (first == null) File(old, MANIFEST).writeText(AppJson.encodeToString(OfflineManifest.serializer(), manifestIn(old)!!.copy(version = "v0")))
            first?.let(server::enqueue)
            server.enqueue(pages(stream(version = "v2")))
            assertEquals(Outcome.NEXT, run())
            val expected = listOfNotNull("/api/files/offline/$id?from=2".takeIf { first != null }, "/api/files/offline/$id?from=0")
            assertEquals(id, expected, expected.map { target() })
            val copy = row(id)
            assertEquals(id, listOf<Any?>(DownloadState.DONE, "v2", 3), listOf(copy.state, copy.copyVersion, copy.copyPageCount))
            assertNotEquals(id, old.name, copy.copyId)
            assertFalse(id, old.exists())
            assertEquals(id, 1, File(dir(copy.copyId), "1").readBytes().first().toInt())
        }
    }

    private data class Case(val name: String, val response: MockResponse, val outcome: Outcome, val state: String, val kind: String?, val error: String?, val attempts: Int = 0)

    @Test
    fun answers() {
        val cases = listOf(
            Case("pageerror", pages(stream(until = 1, error = "page 1: file not found in archive")), Outcome.NEXT, DownloadState.FAILED, ErrorKind.PAGE, "page 1: file not found in archive"),
            Case("cutoff", pages(stream(until = 0, end = false)), Outcome.RETRY, DownloadState.QUEUED, ErrorKind.NETWORK, DownloadError.UNREACHABLE, attempts = 1),
            Case("malformed", pages(Buffer().writeInt(Int.MAX_VALUE)), Outcome.NEXT, DownloadState.FAILED, ErrorKind.PROTOCOL, DownloadError.PROTOCOL),
            Case("error500", voltis(500, "test"), Outcome.NEXT, DownloadState.QUEUED, ErrorKind.SERVER, "test", attempts = 1),
            Case("notfound", voltis(404, "Content not found"), Outcome.NEXT, DownloadState.FAILED, ErrorKind.SERVER, DownloadError.GONE),
            Case("routenotfound", voltis(404, "Not Found"), Outcome.RETRY, DownloadState.QUEUED, ErrorKind.NETWORK, DownloadError.UNREACHABLE),
            Case("changed", voltis(409, "File changed since the last scan"), Outcome.NEXT, DownloadState.FAILED, ErrorKind.SERVER, DownloadError.FILE_CHANGED),
            Case("badrequest", voltis(400, "Content is not a comic"), Outcome.NEXT, DownloadState.FAILED, ErrorKind.SERVER, "Content is not a comic"),
            Case("signedout", voltis(401, "Unauthorized"), Outcome.STOP, DownloadState.QUEUED, null, null),
            Case("portal", MockResponse(200, headersOf("Content-Type", "text/html"), "<html>"), Outcome.RETRY, DownloadState.QUEUED, ErrorKind.NETWORK, DownloadError.UNREACHABLE),
            Case("proxyjson", MockResponse(200, headersOf("Content-Type", "application/json"), "{}"), Outcome.RETRY, DownloadState.QUEUED, ErrorKind.NETWORK, DownloadError.UNREACHABLE),
        )
        for (case in cases) {
            queue(case.name)
            server.enqueue(case.response)
            now += 1
            assertEquals(case.name, case.outcome, run())
            val row = row(case.name)
            assertEquals(case.name, listOf(case.state, case.kind, case.error, case.attempts), listOf(row.state, row.errorKind, row.error, row.attempts))
            assertFalse(case.name, File(temp.root, "downloads").walk().any { it.name.endsWith(".part") })
            // Out of the queue before the next case.
            if (row.state != DownloadState.FAILED) runBlocking { store.pause(case.name) }
        }
        assertEquals(cases.size, server.requestCount)
        // A 500 moves the row to the end of the queue; a 404 is an observation of the item gone, as of its request.
        assertEquals(1_004L, row("error500").queuedAt)
        assertEquals(true to 1_005L, row("notfound").let { it.seenGone to it.seenAt })
        // The parser's message is kept apart from what is shown.
        assertEquals("Manifest too large", row("malformed").errorDetail)
    }

    @Test
    fun aFailingItemDoesNotHoldTheQueue() {
        // Five runs in a row that store nothing fail the item; the next one downloads.
        queue("cut", "next")
        repeat(5) { server.enqueue(pages(stream(until = 0, end = false))) }
        repeat(4) { assertEquals(Outcome.RETRY, run()) }
        assertEquals(Outcome.NEXT, run())
        assertEquals(listOf<Any?>(DownloadState.FAILED, DownloadError.BROKEN_OFF, 5), row("cut").let { listOf(it.state, it.error, it.attempts) })
        server.enqueue(pages(stream()))
        assertEquals(Outcome.NEXT, run())
        assertEquals(DownloadState.DONE, row("next").state)

        // An exception fails only its own item.
        queue("bug", "after")
        broken = "bug"
        assertEquals(null, run())
        assertEquals(listOf<Any?>(DownloadState.FAILED, DownloadError.UNEXPECTED, "java.lang.IllegalStateException: Free space unknown"), row("bug").let { listOf(it.state, it.error, it.errorDetail) })
        server.enqueue(pages(stream()))
        assertEquals(Outcome.NEXT, run())
        assertEquals(DownloadState.DONE, row("after").state)

        // Paused just before its request, which nothing would answer: the call is cancelled without being sent.
        queue("paused")
        val requests = server.requestCount
        checking = { runBlocking { store.pause("paused") } }
        assertEquals(null, run())
        assertEquals(DownloadState.PAUSED, row("paused").state)
        assertEquals(requests, server.requestCount)
    }

    @Test
    fun aFullDiskPausesTheQueue() {
        // The file as listed doesn't fit: nothing is asked for.
        queue("big", "waiting")
        runBlocking { db.content().upsert(listOf(db.content().get("big")!!.copy(fileSize = 100L shl 20))) }
        free = 120L shl 20
        assertEquals(Outcome.NEXT, run())
        assertEquals(DownloadState.FAILED to DownloadError.NO_SPACE, row("big").let { it.state to it.error })
        assertEquals(DownloadState.PAUSED, row("waiting").state)
        assertEquals(0, server.requestCount)

        // A frame larger than what is free.
        runBlocking { store.resume("waiting") }
        free = (50L shl 20) + 2
        server.enqueue(pages(stream(fileSize = null)))
        assertEquals(Outcome.NEXT, run())
        assertEquals(listOf<Any?>(DownloadState.FAILED, ErrorKind.STORAGE, DownloadError.NO_SPACE, 0), row("waiting").let { listOf(it.state, it.errorKind, it.error, it.pagesDone) })
        assertTrue(File(temp.root, "downloads").walk().none { it.name.endsWith(".part") })
    }
}
