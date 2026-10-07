package me.tijlvdb.voltis.data.net

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonObject
import me.tijlvdb.voltis.data.api.UnexpectedResponse
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.api.testApi
import me.tijlvdb.voltis.data.net.VoltisAnswer.Genuine
import me.tijlvdb.voltis.data.net.VoltisAnswer.Unreachable
import me.tijlvdb.voltis.data.reading.RetrofitReadingTransport
import me.tijlvdb.voltis.data.sync.RetrofitPendingTransport
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import okhttp3.Headers.Companion.headersOf
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

/** P2 §10's table: what a response is, raw and through the reading transport. */
class VoltisAnswerTest {
    private val server = MockWebServer()

    @Before
    fun start() = server.start()

    @After
    fun stop() = server.close()

    private fun response(code: Int, type: String, body: String) = MockResponse(code, headersOf("Content-Type", type), body)

    private val json = "application/json"
    private val conflict = """{"message": "reading state changed elsewhere", "state": {"revision": "srv:b", "status": "reading", "progress": {}}, "series": null, "writer": null}"""

    @Test(timeout = 10_000)
    fun answers() = runBlocking {
        val client = OkHttpClient()
        val transport = RetrofitReadingTransport(testApi(server.url("/")))
        val envelope = """{"state": {"revision": null, "status": null, "progress": {}}, "series": null, "writer": null}"""
        for ((response, raw, failure) in listOf(
            Triple(response(200, json, envelope), Genuine(200), null),
            Triple(response(502, json, """{"error": "Bad gateway"}"""), Unreachable, ReadingFailure.Unreachable::class),
            Triple(response(503, "text/html", "<h1>Down</h1>"), Unreachable, ReadingFailure.Unreachable::class),
            Triple(response(504, "text/plain", ""), Unreachable, ReadingFailure.Unreachable::class),
            // A captive portal's page.
            Triple(response(200, "text/html", "<h1>Sign in to the network</h1>"), Unreachable, ReadingFailure.Unreachable::class),
            Triple(response(401, "text/html", "<h1>Sign in</h1>"), Unreachable, ReadingFailure.Unreachable::class),
            Triple(response(401, json, """{"error": "Unauthorized"}"""), Genuine(401, "Unauthorized"), ReadingFailure.SignedOut::class),
            Triple(response(404, json, """{"error": "Content not found"}"""), Genuine(404, "Content not found"), ReadingFailure.Gone::class),
            // Echo's unknown route and a proxy's 404 are JSON with an `error` too: they keep the ops (unreachable), only the content-missing answer drops them.
            Triple(response(404, json, """{"error": "Not Found"}"""), Genuine(404, "Not Found"), ReadingFailure.Unreachable::class),
            Triple(response(404, "text/plain", "404 page not found"), Unreachable, ReadingFailure.Unreachable::class),
            Triple(response(409, json, conflict), Genuine(409, "reading state changed elsewhere"), ReadingFailure.Conflict::class),
            Triple(response(400, json, """{"error": "invalid writer_id"}"""), Genuine(400, "invalid writer_id"), ReadingFailure.Refused::class),
            Triple(response(500, json, """{"error": "Internal Server Error"}"""), Genuine(500, "Internal Server Error"), ReadingFailure.ServerError::class),
            // A Voltis 409 without the state to adopt is no answer the engine can act on.
            Triple(response(409, json, """{"error": "File changed"}"""), Genuine(409, "File changed"), ReadingFailure.Unreachable::class),
            // Genuine by their type, but not what a read asked for: an empty 2xx (Retrofit's null body), pages, an image.
            Triple(MockResponse(204, headersOf(), ""), Genuine(204), ReadingFailure.Unreachable::class),
            Triple(response(200, "application/vnd.voltis.pages", "\u0000\u0000"), Genuine(200), ReadingFailure.Unreachable::class),
            Triple(response(200, "image/jpeg", "not json"), Genuine(200), ReadingFailure.Unreachable::class),
        )) {
            server.enqueue(response)
            val raw2 = client.newCall(Request(server.url("/api/info"))).execute().use { VoltisAnswer.of(it) }
            assertEquals("${response.code} ${response.body}", raw, raw2)
            server.enqueue(response)
            val got = runCatching { transport.get("c_1", quick = true) }.exceptionOrNull()
            assertEquals("${response.code} via the transport", failure, got?.let { it::class })
        }

        // A series action succeeds only with its answer: a portal's page or a malformed body is no answer.
        for ((response, failure) in listOf(
            response(200, json, """{"count": 2}""") to null,
            response(200, "text/html", "<h1>Sign in to the network</h1>") to ReadingFailure.Unreachable::class,
            response(200, json, """{"count": """) to ReadingFailure.Unreachable::class,
        )) {
            server.enqueue(response)
            val got = runCatching { transport.seriesReading("c_s", JsonObject(emptyMap())) }.exceptionOrNull()
            assertEquals("${response.body} via series-reading", failure, got?.let { it::class })
        }

        // The pending rows' transport: the same answers, but a 409 is refused (there is no state to adopt) and a 2xx is the user data.
        val pending = RetrofitPendingTransport(testApi(server.url("/")), "a")
        for ((response, failure) in listOf(
            response(200, json, """{"starred": true, "rating": 4, "notes": null}""") to null,
            response(200, "text/html", "<h1>Sign in to the network</h1>") to ReadingFailure.Unreachable::class,
            response(400, json, """{"error": "invalid rating"}""") to ReadingFailure.Refused::class,
            response(409, json, conflict) to ReadingFailure.Refused::class,
            response(404, json, """{"error": "Content not found"}""") to ReadingFailure.Gone::class,
            response(404, json, """{"error": "Not Found"}""") to ReadingFailure.Unreachable::class,
            response(500, json, """{"error": "Internal Server Error"}""") to ReadingFailure.ServerError::class,
            response(401, json, """{"error": "Unauthorized"}""") to ReadingFailure.SignedOut::class,
            response(503, "text/html", "<h1>Down</h1>") to ReadingFailure.Unreachable::class,
            // Valid JSON that isn't the user data: no more Voltis' than HTML is.
            response(200, json, """[1]""") to ReadingFailure.Unreachable::class,
        )) {
            server.enqueue(response)
            val got = runCatching { pending.updateUserData("c_1", JsonObject(emptyMap())) }
            assertEquals("${response.code} via the pending transport", failure, got.exceptionOrNull()?.let { it::class })
            if (failure == null) assertEquals(UserData(starred = true, rating = 4), got.getOrNull())
        }

        // A conflict carries the server's state.
        server.enqueue(response(409, json, conflict))
        val lost = runCatching { transport.post("c_1", JsonObject(emptyMap())) }.exceptionOrNull()
        assertEquals("srv:b", (lost as ReadingFailure.Conflict).current.state.revision)

        // No answer at all.
        val nobody = RetrofitReadingTransport(testApi("http://127.0.0.1:1/".toHttpUrl()))
        assertTrue(runCatching { nobody.get("c_1", quick = false) }.exceptionOrNull() is ReadingFailure.Unreachable)

        // A WebSocket upgrade is an answer only to a request that asked for one (roadmap Phase 4).
        for ((upgrade, answer) in listOf("websocket" to Genuine(101), null to Unreachable)) {
            val request = Request.Builder().url(server.url("/api/ws")).apply { upgrade?.let { header("Upgrade", it) } }.build()
            val switching = Response.Builder().request(request).protocol(Protocol.HTTP_1_1).code(101).message("Switching Protocols").build()
            assertEquals(upgrade, answer, VoltisAnswer.of(switching))
        }
    }

    @Test(timeout = 10_000)
    fun mismatchedJsonIsUnreachableButNamed() = runBlocking {
        val api = testApi(server.url("/"))
        // Valid JSON of the wrong shape: unreachable for every fallback, but named for what the user is shown.
        server.enqueue(response(200, json, "[1]"))
        val mismatch = runCatching { api.info() }.exceptionOrNull()
        assertTrue(mismatch.toString(), mismatch is UnexpectedResponse)
        assertTrue(mismatch!!.isUnreachable())

        // HTML, a truncated body and an empty one aren't JSON: unreachable, and not named.
        for (body in listOf("<h1>Sign in to the network</h1>", """{"version": """, "")) {
            server.enqueue(response(200, json, body))
            val failure = runCatching { api.info() }.exceptionOrNull()!!
            assertTrue("$body: $failure", failure.isUnreachable() && failure !is UnexpectedResponse)
        }
    }
}
