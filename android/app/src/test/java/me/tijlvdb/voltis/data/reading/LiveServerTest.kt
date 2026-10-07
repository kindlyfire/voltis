package me.tijlvdb.voltis.data.reading

import java.security.SecureRandom
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.TokenRequest
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.content.ContentListParams
import me.tijlvdb.voltis.domain.reading.ComicAdapter
import me.tijlvdb.voltis.domain.reading.CommandOutcome
import me.tijlvdb.voltis.domain.reading.InMemoryReadingStore
import me.tijlvdb.voltis.domain.reading.Outcome
import me.tijlvdb.voltis.domain.reading.ReaderNotice
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.ReadingSession
import me.tijlvdb.voltis.domain.reading.ReadingState
import me.tijlvdb.voltis.domain.reading.ReadingTransport
import me.tijlvdb.voltis.domain.reading.SyncNotice
import me.tijlvdb.voltis.domain.reading.position
import me.tijlvdb.voltis.domain.sync.NoticeKind
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assume.assumeTrue
import org.junit.Before
import org.junit.Test
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * Opt-in (P2 §14): the transport and the engine against a real server, named by `VOLTIS_TEST_SERVER`
 * with `VOLTIS_TEST_USER` and `VOLTIS_TEST_PASSWORD`. It writes only to the made-up series "Quartz
 * Lantern" (or `VOLTIS_TEST_SERIES`) and its volumes, which it clears before and after.
 */
class LiveServerTest {
    private val base: String? = System.getenv("VOLTIS_TEST_SERVER")
    private var token: String? = null
    private lateinit var api: VoltisApi
    private lateinit var transport: RetrofitReadingTransport
    private lateinit var series: String
    private lateinit var volumes: List<String>
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    /** This run's reader, and the "web": another device with a writer of its own. */
    private val ours = writerId()
    private val web = writerId()
    private var webSeq = 0L

    @Before
    fun connect() = runBlocking<Unit> {
        assumeTrue("VOLTIS_TEST_SERVER is not set", base != null)
        val client = OkHttpClient.Builder()
            .addInterceptor { chain -> chain.proceed(token?.let { chain.request().newBuilder().header("Authorization", "Bearer $it").build() } ?: chain.request()) }
            .build()
        api = Retrofit.Builder().baseUrl(base!!.trimEnd('/') + "/").client(client)
            .addConverterFactory(AppJson.asConverterFactory("application/json".toMediaType())).build().create(VoltisApi::class.java)
        token = api.token(TokenRequest(System.getenv("VOLTIS_TEST_USER"), System.getenv("VOLTIS_TEST_PASSWORD"), "LiveServerTest")).token
        transport = RetrofitReadingTransport(api)
        series = System.getenv("VOLTIS_TEST_SERIES") ?: api.content(ContentListParams(search = "Quartz Lantern").toQuery()).data
            .single { it.title == "Quartz Lantern" && it.type == ContentType.COMIC_SERIES }.id
        volumes = api.content(ContentListParams.volumes(series).toQuery()).data.map { it.id }
        check(volumes.size == 3) { "Quartz Lantern should have three volumes" }
        clearSeries()
    }

    @After
    fun disconnect() = runBlocking<Unit> {
        scope.cancel()
        if (token == null) return@runBlocking
        try {
            clearSeries()
        } finally {
            api.logout()
        }
    }

    private suspend fun clearSeries() = transport.post(series, body("clear"))

    private fun body(op: String, vararg fields: Pair<String, JsonElement>) = JsonObject(mapOf("op" to JsonPrimitive(op)) + fields)

    private fun writer(id: String, seq: Long) = arrayOf("writer_id" to JsonPrimitive(id), "seq" to JsonPrimitive(seq))

    private suspend fun stateOf(id: String) = transport.get(id, quick = false).state

    private fun ReadingState.base() = "base_revision" to (revision?.let(::JsonPrimitive) ?: JsonNull)

    /** The web reads [id] to [page], on top of what the server has. */
    private suspend fun webReads(id: String, page: Int) {
        transport.post(id, body("position", "progress" to position(page, PAGES), stateOf(id).base(), *writer(web, ++webSeq)))
    }

    private fun engine(store: InMemoryReadingStore = InMemoryReadingStore(), reachable: () -> Boolean = { true }): ReadingEngine {
        val cut = object : ReadingTransport by transport {
            private fun check() {
                if (!reachable()) throw ReadingFailure.Unreachable()
            }

            override suspend fun get(contentId: String, quick: Boolean) = check().let { transport.get(contentId, quick) }

            override suspend fun post(contentId: String, body: JsonObject) = check().let { transport.post(contentId, body) }
        }
        return ReadingEngine(
            store, cut, EngineTest.FakeWriter(ours), CatalogEvents(), EngineTest.FakeConnectivity(reachable), EngineTest.FakeScheduler(), scope,
            main = Dispatchers.IO, onError = { throw it },
        ).also { it.start() }
    }

    private suspend fun content(id: String): Content = api.contentById(id)

    /** A reader of [id] that records its placements and notices. */
    private suspend fun reader(engine: ReadingEngine, id: String, placed: MutableList<Int>, notices: MutableList<ReaderNotice>): ReadingSession {
        val item = content(id)
        val session = engine.attach(id, ComicAdapter({ PAGES }, { item }) { synchronized(placed) { placed += it } })
        scope.launch { session.notices.collect { synchronized(notices) { notices += it } } }
        session.load()
        return session
    }

    /** Waits for [block] to give something. */
    private suspend fun <T : Any> eventually(block: () -> T?): T = withTimeout(10_000) {
        var value = block()
        while (value == null) {
            delay(50)
            value = block()
        }
        value
    }

    @Test
    fun positionsRoundTripAndRepeatsAreAnsweredNone() = live {
        val (v1) = volumes
        val engine = engine()
        val placed = mutableListOf<Int>()
        val session = reader(engine, v1, placed, mutableListOf())
        session.moved(position(3, PAGES))
        session.flush()
        engine.drain()
        val saved = stateOf(v1)
        assertEquals(3, saved.progress.page())
        assertEquals(ours, saved.revision?.substringBefore(':'))
        // The web reads on: the reader follows when it checks.
        webReads(v1, 5)
        session.check()
        assertEquals(listOf(5), eventually { synchronized(placed) { placed.toList().takeIf { it.isNotEmpty() } } })
        session.detach()
        engine.stop()

        // A repeat with the same (writer, seq) is answered "none" and changes nothing.
        val repeat = body("position", "progress" to position(1, PAGES), stateOf(v1).base(), *writer(web, ++webSeq))
        transport.post(v1, repeat)
        val after = stateOf(v1)
        assertEquals(Outcome.NONE, transport.post(v1, repeat).outcome)
        assertEquals(after, stateOf(v1))
    }

    @Test
    fun seriesOpsRepeatedAreIgnoredAndLeaveOurOwn409() = live {
        val (v1, v2) = volumes
        val seq = 100L
        val markThrough = JsonObject(mapOf("action" to JsonPrimitive("mark_through"), "until_id" to JsonPrimitive(v2)) + writer(ours, seq))
        transport.seriesReading(series, markThrough)
        assertEquals(listOf("completed", "completed"), listOf(v1, v2).map { stateOf(it).status })
        // Another device sets v1 back to Reading; the repeat with the same seq doesn't complete it again.
        transport.post(v1, body("set_status", "status" to JsonPrimitive("reading")))
        transport.seriesReading(series, markThrough)
        assertEquals("reading", stateOf(v1).status)

        // A write of ours on a base that our own series op has rewritten since: a 409 whose writer is ours.
        val v2before = stateOf(v2)
        transport.seriesReading(series, JsonObject(mapOf("action" to JsonPrimitive("clear")) + writer(ours, seq + 1)))
        val own = runCatching { transport.post(v2, body("position", "progress" to position(1, PAGES), v2before.base(), *writer(ours, seq + 2))) }.exceptionOrNull()
        assertEquals(ours, (own as ReadingFailure.Conflict).current.writer)
        // Another device's write: a 409 that names it.
        webReads(v2, 3)
        val foreign = runCatching { transport.post(v2, body("position", "progress" to position(4, PAGES), own.current.state.base(), *writer(ours, seq + 3))) }.exceptionOrNull()
        assertEquals(web, (foreign as ReadingFailure.Conflict).current.writer)
    }

    @Test
    fun commandsThroughTheEngine() = live {
        val (v1, v2, v3) = volumes
        var reachable = true
        val store = InMemoryReadingStore()
        val engine = engine(store) { reachable }

        // Clear.
        webReads(v1, 4)
        assertEquals(CommandOutcome.APPLIED, engine.command(content(v1), body("clear")))
        assertEquals(null to JsonObject(emptyMap()), stateOf(v1).let { it.status to it.progress })

        // A destructive command made without the server and overtaken elsewhere is dropped, with a notice.
        webReads(v2, 2)
        val page = content(v2)
        reachable = false
        assertEquals(CommandOutcome.QUEUED, engine.command(page, body("mark_completed")))
        webReads(v2, 4)
        reachable = true
        engine.drain()
        assertEquals("reading" to 4, stateOf(v2).let { it.status to it.progress.page() })
        assertEquals(listOf(NoticeKind.CHANGED_ELSEWHERE to "mark_completed"), store.notices.map { it.kind to it.detail.command })

        // A plain status wins over another device's.
        val fetched = content(v3)
        transport.post(v3, body("set_status", "status" to JsonPrimitive("on_hold")))
        assertEquals(CommandOutcome.APPLIED, engine.command(fetched, body("set_status", "status" to JsonPrimitive("dropped"))))
        assertEquals("dropped", stateOf(v3).status)

        // Reading a dropped volume offers Undo, which restores it; the series' status goes through the item.
        val notices = mutableListOf<ReaderNotice>()
        val session = reader(engine, v3, mutableListOf(), notices)
        session.moved(position(2, PAGES))
        session.flush()
        engine.drain()
        assertEquals("reading", stateOf(v3).status)
        val undo = eventually { synchronized(notices) { notices.firstOrNull { it.notice is SyncNotice.UndoOffer } } }
        undo.action!!()
        engine.drain()
        assertEquals("dropped", stateOf(v3).status)
        assertEquals(CommandOutcome.APPLIED, session.seriesCommand("on_hold"))
        assertEquals("on_hold", transport.get(v3, quick = false).series?.status)
        session.detach()
        assertEquals(0, engine.unsent.first())
        assertNull(store.ops.firstOrNull())
        engine.stop()
    }

    private fun live(block: suspend () -> Unit) = runBlocking { withTimeout(60_000) { block() } }

    private fun JsonObject.page() = (this["current_page"] as? JsonPrimitive)?.content?.toInt()

    private companion object {
        const val PAGES = 6

        fun writerId(): String {
            val random = SecureRandom()
            return "t" + (1..16).map { "0123456789abcdefghijklmnopqrstuvwxyz"[random.nextInt(36)] }.joinToString("")
        }
    }
}
