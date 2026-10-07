package me.tijlvdb.voltis.data.api

import kotlinx.coroutines.test.runTest
import me.tijlvdb.voltis.data.reading.RetrofitReadingTransport
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import okhttp3.HttpUrl.Companion.toHttpUrl
import me.tijlvdb.voltis.data.auth.Server
import me.tijlvdb.voltis.data.auth.SessionState
import me.tijlvdb.voltis.data.auth.SessionStore
import okhttp3.Headers
import me.tijlvdb.voltis.data.auth.testStore
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import okhttp3.OkHttpClient
import okhttp3.Request
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class AuthInterceptorTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private val stored = MockWebServer()
    private val other = MockWebServer()

    @Before
    fun start() {
        stored.start()
        other.start()
    }

    @After
    fun stop() {
        stored.close()
        other.close()
    }

    /** A request made for a session (a queued logout) keeps its server and bearer when the same account signs in again meanwhile. */
    @Test
    fun aRequestForASessionKeepsItsBearerAcrossAReLogin() = runTest {
        val store = testStore(tmp.newFolder().resolve("s.preferences_pb"))
        val client = OkHttpClient.Builder()
            .addInterceptor(ServerUrlInterceptor(store))
            .addInterceptor(AuthInterceptor(store) { true })
            .build()
        store.connect("srv", stored.url("/"))
        store.signIn("srv", "t1", "u1", "alice")
        val sent = store.active()!!
        store.signIn("srv", "t2", "u1", "alice")
        stored.enqueue(MockResponse(code = 200))
        client.newCall(Request.Builder().url("http://$PLACEHOLDER_HOST/api/auth/logout").tag(SessionStore.Active::class.java, sent).build()).execute().close()
        assertEquals("Bearer t1", stored.takeRequest().headers["Authorization"])
    }

    @Test
    fun bearerOnlyForStoredServer() = runTest {
        val store = testStore(tmp.newFolder().resolve("s.preferences_pb"))
        var probes = 0
        var probePasses = true
        val client = OkHttpClient.Builder()
            .addInterceptor(ServerUrlInterceptor(store))
            .addInterceptor(AuthInterceptor(store) { probes++; probePasses })
            .build()
        fun get(url: String) = client.newCall(Request.Builder().url(url).build()).execute().use { it.code }
        val voltis401 = MockResponse(code = 401, body = """{"error": "Unauthorized"}""")

        val base = stored.url("/voltis")
        val server = Server("srv", base)
        store.connect("srv", base)
        store.signIn("srv", "t1", "u1", "alice")

        // Placeholder requests go to the stored server, under its path, with the bearer.
        stored.enqueue(MockResponse(code = 200))
        assertEquals(200, get("http://$PLACEHOLDER_HOST/api/users/me"))
        stored.takeRequest().let {
            assertEquals("/voltis/api/users/me", it.url.encodedPath)
            assertEquals("Bearer t1", it.headers["Authorization"])
        }

        // An absolute URL elsewhere is neither rewritten nor authenticated, and its 401 changes nothing.
        other.enqueue(voltis401)
        assertEquals(401, get(other.url("/api/info").toString()))
        other.takeRequest().let {
            assertEquals("/api/info", it.url.encodedPath)
            assertNull(it.headers["Authorization"])
        }
        val signedIn = SessionState.SignedIn(server, "u1", "alice")
        assertEquals(signedIn, store.state.value)

        // A portal's bare 401, and a redirect to another origin answering Voltis' 401 (OkHttp drops the bearer there), keep it.
        stored.enqueue(MockResponse(code = 401))
        assertEquals(401, get("http://$PLACEHOLDER_HOST/api/users/me"))
        stored.enqueue(MockResponse(code = 302, headers = Headers.headersOf("Location", other.url("/elsewhere").toString())))
        other.enqueue(voltis401)
        assertEquals(401, get("http://$PLACEHOLDER_HOST/api/users/me"))
        assertNull(other.takeRequest().headers["Authorization"])
        assertEquals(listOf(signedIn, 0), listOf(store.state.value, probes))

        // Voltis' 401 with a probe that fails (another server at the address) keeps it too.
        probePasses = false
        stored.enqueue(voltis401)
        assertEquals(401, get("http://$PLACEHOLDER_HOST/api/users/me"))
        assertEquals(listOf(signedIn, 1), listOf(store.state.value, probes))

        // With a passing probe it drops the token but keeps the user.
        probePasses = true
        stored.enqueue(voltis401)
        assertEquals(401, get("http://$PLACEHOLDER_HOST/api/users/me"))
        assertEquals(SessionState.NeedsReauth(server, "u1"), store.state.value)

        // A late 401 for the old token leaves a newer sign-in alone, and another record holding the same token too.
        store.connect("srv2", other.url("/"))
        store.signIn("srv2", "t2", "u1", "alice")
        store.connect("srv", base)
        store.signIn("srv", "t2", "u1", "alice")
        store.markNeedsReauth(SessionStore.Active(server, "t1", "u1"))
        assertEquals("t2", store.active()?.token)
        store.markNeedsReauth(SessionStore.Active(server, "t2", "u1"))
        assertNull(store.active()?.token)
        assertEquals("t2", store.server("srv2")?.let { store.connect(it.id, it.url); store.active()?.token })
    }

    @Test
    fun aReadingRequestOfAnotherAccountIsNeverSent() = runTest {
        val store = testStore(tmp.newFolder().resolve("s.preferences_pb"))
        val client = OkHttpClient.Builder()
            .addInterceptor(ServerUrlInterceptor(store))
            .addInterceptor(AuthInterceptor(store) { true })
            .build()
        val api = testApi("http://$PLACEHOLDER_HOST/".toHttpUrl(), client)
        store.connect("srv", stored.url("/"))
        store.signIn("srv", "t1", "u1", "alice")
        val alices = RetrofitReadingTransport(api, "srv/u1")

        // Another user signs in before alice's engine's request executes: it never leaves.
        store.signIn("srv", "t2", "u2", "bob")
        val refused = runCatching { alices.get("c_1", quick = false) }.exceptionOrNull()
        assertTrue(refused is ReadingFailure.Unreachable && refused.cause is AccountChangedException)

        // Bob's go out with his credentials.
        stored.enqueue(MockResponse(code = 200, body = """{"state": {}}"""))
        RetrofitReadingTransport(api, "srv/u2").get("c_1", quick = false)
        assertEquals("Bearer t2", stored.takeRequest().headers["Authorization"])
        assertEquals(1, stored.requestCount)
    }
}
