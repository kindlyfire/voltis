package me.tijlvdb.voltis.data.content

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.File
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.AccountChangedException
import me.tijlvdb.voltis.data.api.AuthInterceptor
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.api.PLACEHOLDER_HOST
import me.tijlvdb.voltis.data.api.ServerUrlInterceptor
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.api.testApi
import me.tijlvdb.voltis.data.auth.testStore
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.db.importReading
import me.tijlvdb.voltis.data.downloads.cache
import me.tijlvdb.voltis.data.reading.ReadingIngest
import me.tijlvdb.voltis.data.reading.RoomReadingStore
import me.tijlvdb.voltis.domain.catalog.offlineContinue
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.RequestedBy
import me.tijlvdb.voltis.domain.reading.Change
import me.tijlvdb.voltis.domain.reading.Lane
import me.tijlvdb.voltis.domain.reading.Op
import me.tijlvdb.voltis.domain.reading.OpKind
import me.tijlvdb.voltis.domain.reading.ReadingSnapshot
import me.tijlvdb.voltis.domain.reading.ReadingState
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** Cached pages and the offline Continue read the effective reading (newest snapshot, unsent ops over it), not lanes; ingestion is bound to its account. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [37], application = Application::class)
class ReadingCatalogTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private val dbs = mutableListOf<VoltisDatabase>()
    private val server = MockWebServer()

    private fun database() = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), VoltisDatabase::class.java).build().also { dbs += it }

    @After
    fun close() {
        dbs.forEach { it.close() }
        server.close()
    }

    private fun page(n: Int) = JsonObject(mapOf("current_page" to JsonPrimitive(n)))

    private fun volume(id: String, parent: String? = "s", type: String = ContentType.COMIC, userData: UserData? = null) = Content(id, id.uppercase(), type, parentId = parent, userData = userData)

    private suspend fun VoltisDatabase.download(id: String) =
        downloads().insert(listOf(DownloadEntity(id, id, DownloadState.DONE, requestedBy = RequestedBy.USER, queuedAt = 1, copyId = "copy-$id")))

    /**
     * A series whose volume B was last read, and a held lane whose base is older than what the phone has since learned
     * (another device cleared B) with a position queued on it: the page, the picker and the series' Continue all show
     * the snapshot with the op over it, a newer import reaches the cached page, and the lane's own projection shows nowhere.
     */
    @Test(timeout = 20_000)
    fun cachedPagesShowTheEffectiveReading() = runBlocking {
        val db = database()
        val read = UserData(status = "reading", progress = page(2), revision = "srv:3", readingSeq = 3, lastReadAt = "2026-01-02T00:00:00Z")
        db.cache(
            listOf(Content("s", "Series", ContentType.COMIC_SERIES, userData = UserData(status = "reading", revision = "srv:1", readingSeq = 1)) to null, volume("a") to 0, volume("b", userData = read) to 1),
            fetchedAt = 1,
        ) { false }
        db.content().setVolumesKnown("s", true)
        val store = RoomReadingStore(db) {}
        store.commit(Change(lanes = listOf(Lane("b", parentId = "s", type = ContentType.COMIC, state = ReadingState("srv:1", "completed", seq = 1), needsReview = true))))
        // Another device cleared B (seq 9); the phone has a position of its own queued.
        db.importReading(listOf(ReadingSnapshot("b", ReadingState("srv:9", seq = 9))))
        store.commit(Change(ops = listOf(Op(contentId = "b", kind = OpKind.POSITION, payload = page(4), createdAt = 1000))))
        val catalog = DownloadedCatalog { db }

        val stored = catalog.stored("b")!!.userData!!
        assertEquals(listOf("reading", page(4), 9L), listOf(stored.status, stored.progress, stored.readingSeq))
        // The picker's volumes show it too, with the cached series' order; and the series' Continue resumes at B.
        assertEquals(listOf(null, "reading"), catalog.volumes("s")!!.map { it.userData?.status })
        val series = catalog.series("s").first()!!
        assertEquals(listOf("b", "resume"), offlineContinue("s", series).let { listOf(it.target?.id, it.action) })

        // A newer state imported later reaches an observer of the item, without the reader moving.
        val seen = async { catalog.effective(setOf("b")).first { it["b"]?.seq == 10L } }
        db.importReading(listOf(ReadingSnapshot("b", ReadingState("srv:10", "completed", progress = page(9), seq = 10))))
        // A position over a completed item leaves it completed.
        assertEquals("completed", withTimeout(5_000) { seen.await() }.getValue("b").status)
    }

    /**
     * The series grid publishes `withReading(effective[id])` over its fetched or cached volumes. With a retained Completed lane
     * base, a newer clear snapshot and a queued position, a volume card shows Reading at page 4 (not the lane's Completed),
     * and a later snapshot-only import reaches the observed list.
     */
    @Test(timeout = 20_000)
    fun aSeriesGridShowsTheEffectiveReading() = runBlocking {
        val db = database()
        val done = UserData(status = "completed", progress = page(9), revision = "srv:2", readingSeq = 2, lastReadAt = "2026-01-02T00:00:00Z")
        db.cache(listOf(Content("s", "Series", ContentType.COMIC_SERIES) to null, volume("a", userData = done) to 0, volume("b", userData = done) to 1), fetchedAt = 1) { false }
        db.content().setVolumesKnown("s", true)
        val store = RoomReadingStore(db) {}
        store.commit(Change(lanes = listOf(Lane("b", parentId = "s", type = ContentType.COMIC, state = ReadingState("srv:2", "completed", progress = page(9), seq = 2), needsReview = true))))
        db.importReading(listOf(ReadingSnapshot("b", ReadingState("srv:9", seq = 9))))
        store.commit(Change(ops = listOf(Op(contentId = "b", kind = OpKind.POSITION, payload = page(4), createdAt = 1000))))
        val catalog = DownloadedCatalog { db }
        fun List<Content>.shown(readings: Map<String, me.tijlvdb.voltis.domain.reading.EffectiveReading>) =
            map { it.withReading(readings[it.id]).userData?.let { u -> u.status to u.progress } }
        val ids = setOf("a", "b")

        // The cached fallback (a list as it was fetched, overlaid again as the grid does) and a fresh online list alike.
        val fetched = listOf(volume("a", userData = done), volume("b", userData = done))
        val expected = listOf("completed" to page(9), "reading" to page(4))
        assertEquals(expected, catalog.volumes("s")!!.shown(catalog.effective(ids).first()))
        assertEquals(expected, fetched.shown(catalog.effective(ids).first()))

        // A snapshot-only import reaches the observed readings: B was completed elsewhere (a position leaves it so).
        val seen = async { catalog.effective(ids).first { it["b"]?.seq == 10L } }
        db.importReading(listOf(ReadingSnapshot("b", ReadingState("srv:10", "completed", progress = page(9), seq = 10))))
        assertEquals("completed" to page(4), fetched.shown(withTimeout(5_000) { seen.await() })[1]?.let { it.first to it.second })
    }

    /** What the shortcut opens offline comes from snapshots and ops: no lane needed, and a held older lane changes nothing. */
    @Test
    fun theOfflineShortcutReadsTheEffectiveReading() = runBlocking {
        val db = database()
        fun reading(at: String, seq: Long) = UserData(status = "reading", progress = page(1), revision = "srv:$seq", readingSeq = seq, lastReadAt = at)
        db.cache(
            listOf(
                volume("book", null, ContentType.BOOK, reading("2026-01-02T00:00:00Z", 2)) to null,
                volume("copy", null, userData = reading("2026-01-03T00:00:00Z", 3)) to null,
                // No complete copy: not offered.
                volume("bare", null, userData = reading("2026-01-04T00:00:00Z", 4)) to null,
                volume("done", null, ContentType.BOOK, UserData(status = "completed", readingSeq = 5, revision = "srv:5", lastReadAt = "2026-01-05T00:00:00Z")) to null,
            ),
            fetchedAt = 1,
        ) { false }
        db.download("copy")
        // The copy's lane is held at an older, completed base: it has no say.
        val store = RoomReadingStore(db) {}
        store.commit(Change(lanes = listOf(Lane("copy", type = ContentType.COMIC, state = ReadingState("srv:1", "completed", seq = 1), needsReview = true))))
        fun order() = runBlocking { db.continueCandidates().sortedByDescending { it.recency }.map { it.contentId } }
        assertEquals(listOf("copy", "book"), order())
        // Another device finished the copy, and the phone learned it, without any lane being touched.
        db.importReading(listOf(ReadingSnapshot("copy", ReadingState("srv:8", "completed", seq = 8))))
        assertEquals(listOf("book"), order())
        // A position queued on the book is later than anything the server stated.
        db.importReading(listOf(ReadingSnapshot("copy", ReadingState("srv:9", "reading", progress = page(3), lastReadAt = "2026-01-06T00:00:00Z", seq = 9))))
        assertEquals(listOf("copy", "book"), order())
        store.commit(Change(ops = listOf(Op(contentId = "book", kind = OpKind.POSITION, payload = page(5), createdAt = 1000))))
        assertEquals(listOf("book", "copy"), order())
    }

    /**
     * A request is made for the account of the store it is imported into, and refused as it leaves when the session is another
     * one's: also in the interval where the session has switched and the store hasn't yet. An explicit reader account binds the same way.
     */
    @Test
    fun ingestionIsBoundToTheAccountsRequest() = runBlocking {
        server.start()
        val session = testStore(tmp.newFolder().resolve("s.preferences_pb"))
        val client = OkHttpClient.Builder().addInterceptor(ServerUrlInterceptor(session)).addInterceptor(AuthInterceptor(session) { true }).build()
        session.connect("srv", server.url("/"))
        session.signIn("srv", "t1", "u1", "alice")
        val a = AccountStore("srv/u1", File(tmp.root, "a"), database())
        val b = AccountStore("srv/u2", File(tmp.root, "b"), database())
        var current: AccountStore? = a
        val repository = ContentRepository(testApi("http://$PLACEHOLDER_HOST/".toHttpUrl(), client), ReadingIngest { current })
        val body = """{"id": "c1", "title": "Tin Lantern", "type": "comic", "user_data": {"status": "completed", "reading_seq": "7", "revision": "srv:7"}}"""

        server.enqueue(MockResponse(body = body))
        repository.get("c1")
        assertEquals(7L, a.db.snapshots().get("c1")?.seq)

        // The session is B's, the open store still A's: nothing is sent, and nothing of B's is imported into A.
        session.signIn("srv", "t2", "u2", "bob")
        val failure = runCatching { repository.get("c2") }.exceptionOrNull()
        assertTrue(failure.toString(), failure is AccountChangedException)
        assertTrue(runCatching { repository.continueTarget("c2") }.exceptionOrNull() is AccountChangedException)
        assertEquals(1, server.requestCount)
        assertNull(a.db.snapshots().get("c2"))

        // The store has followed: B's answer is B's. A reader opened for A (explicit) while B is open imports nothing and sends nothing.
        current = b
        server.enqueue(MockResponse(body = body.replace("c1", "c2")))
        repository.get("c2")
        assertEquals(listOf(null, 7L), listOf(a.db.snapshots().get("c2"), b.db.snapshots().get("c2")?.seq))
        assertTrue(runCatching { repository.comicWithPageSizes("c3", ForAccount("srv/u1")) }.exceptionOrNull() is AccountChangedException)
        assertEquals(2, server.requestCount)
    }
}
