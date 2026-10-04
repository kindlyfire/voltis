package me.tijlvdb.voltis.data.api

import kotlinx.coroutines.test.runTest
import me.tijlvdb.voltis.data.auth.Server
import me.tijlvdb.voltis.data.auth.SessionState
import me.tijlvdb.voltis.data.auth.testStore
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import okhttp3.OkHttpClient
import okhttp3.Request
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
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

    @Test
    fun bearerOnlyForStoredServer() = runTest {
        val store = testStore(tmp.newFolder().resolve("s.preferences_pb"))
        val client = OkHttpClient.Builder()
            .addInterceptor(ServerUrlInterceptor(store))
            .addInterceptor(AuthInterceptor(store))
            .build()
        fun get(url: String) = client.newCall(Request.Builder().url(url).build()).execute().use { it.code }

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
        other.enqueue(MockResponse(code = 401))
        assertEquals(401, get(other.url("/api/info").toString()))
        other.takeRequest().let {
            assertEquals("/api/info", it.url.encodedPath)
            assertNull(it.headers["Authorization"])
        }
        assertEquals(SessionState.SignedIn(server, "u1", "alice"), store.state.value)

        // A 401 for the bearer drops the token but keeps the user.
        stored.enqueue(MockResponse(code = 401))
        assertEquals(401, get("http://$PLACEHOLDER_HOST/api/users/me"))
        assertEquals(SessionState.NeedsReauth(server, "u1"), store.state.value)

        // A late 401 for the old token leaves a newer sign-in alone.
        store.signIn("srv", "t2", "u1", "alice")
        store.markNeedsReauth("t1")
        assertEquals("t2", store.active()?.token)
    }
}
