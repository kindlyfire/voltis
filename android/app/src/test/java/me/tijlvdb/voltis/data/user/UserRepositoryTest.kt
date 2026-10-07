package me.tijlvdb.voltis.data.user

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.LibraryVisibility
import me.tijlvdb.voltis.data.api.Me
import me.tijlvdb.voltis.data.api.User
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.api.testApi
import me.tijlvdb.voltis.data.auth.testStore
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class UserRepositoryTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private val server = MockWebServer()

    @Before
    fun start() = server.start()

    @After
    fun stop() = server.close()

    private fun me(method: String) = MockResponse(body = """{"id": "u1", "username": "alice", "session_method": "$method"}""")

    // Bounded: a regression would otherwise wait forever on a value that never comes.
    @Test(timeout = 10_000)
    fun signingInAgainAsTheSameUserStartsOver() = runBlocking {
        val store = testStore(tmp.newFolder().resolve("s.preferences_pb"))
        val api = testApi(server.url("/"))
        val events = CatalogEvents()
        val users = UserRepository(api, store, events, CoroutineScope(SupervisorJob() + Dispatchers.Default))
        store.connect("srv", server.url("/"))

        // A session loads its user by itself.
        server.enqueue(me("password"))
        store.signIn("srv", "t1", "u1", "alice")
        assertEquals("password", users.me.first { it != null }?.sessionMethod)

        store.signOut(store.active()!!)
        users.me.first { it == null }

        // The same user again, and the new session's fetch fails: nothing of the old session shows.
        server.enqueue(MockResponse(code = 500))
        store.signIn("srv", "t2", "u1", "alice")
        repeat(2) { server.takeRequest() }
        assertNull(users.me.value)

        server.enqueue(me("oidc"))
        users.refresh()
        assertEquals("oidc", users.me.first { it != null }?.sessionMethod)
        server.takeRequest()

        // A preferences patch goes out with its nulls. Its answer is a plain user: only the preferences are taken over.
        server.enqueue(MockResponse(body = """{"id": "u1", "username": "alice", "preferences": {"libraries": {"l_b": {"visibility": "hide"}}}}"""))
        val changed = async(start = CoroutineStart.UNDISPATCHED) { events.changes.first() }
        users.patchPreferences(JsonObject(mapOf("libraries" to JsonObject(mapOf("l_a" to JsonNull)))))
        val request = server.takeRequest()
        assertEquals(
            Triple("PATCH", "/api/users/me/preferences", """{"libraries":{"l_a":null}}"""),
            Triple(request.method, request.url.encodedPath, request.body?.utf8()),
        )
        assertEquals("oidc", users.me.first { it?.prefs?.libraryVisibility("l_b") == LibraryVisibility.HIDE }?.sessionMethod)
        assertEquals(CatalogChange.PreferencesChanged, changed.await())
    }

    /** Answers the first me() at once and later ones when the test says so. */
    private class Api(real: VoltisApi) : VoltisApi by real {
        var meCalls = 0
        var patches = 0
        val laterMe = CompletableDeferred<Me>()

        override suspend fun me(): Me = if (meCalls++ == 0) user(LibraryVisibility.SHOW) else laterMe.await()

        override suspend fun patchPreferences(patch: JsonObject): User {
            patches++
            return User(prefs(LibraryVisibility.HIDE))
        }
    }

    @Test
    fun aPatchWaitsForARefreshThatStartedBeforeIt() = runTest {
        val store = testStore(tmp.newFolder().resolve("s.preferences_pb"))
        store.connect("srv", server.url("/"))
        store.signIn("srv", "t1", "u1", "alice")
        store.state.first { it.account != null }
        val api = Api(testApi())
        // Background work only runs on runCurrent(): advanceUntilIdle() stops once the test itself is idle.
        val users = UserRepository(api, store, CatalogEvents(), backgroundScope)
        runCurrent()
        assertEquals(LibraryVisibility.SHOW, users.me.value?.prefs?.libraryVisibility("l_a"))

        launch { users.refresh() }
        runCurrent()
        assertEquals(2, api.meCalls)
        launch { users.patchPreferences(prefs(LibraryVisibility.HIDE)) }
        runCurrent()
        assertEquals(0, api.patches)

        // The refresh's older answer lands first; the patch's newer one stays.
        api.laterMe.complete(user(LibraryVisibility.SHOW))
        runCurrent()
        assertEquals(1, api.patches)
        assertEquals(LibraryVisibility.HIDE, users.me.value?.prefs?.libraryVisibility("l_a"))
    }

    private companion object {
        fun prefs(visibility: String) =
            JsonObject(mapOf("libraries" to JsonObject(mapOf("l_a" to JsonObject(mapOf("visibility" to JsonPrimitive(visibility)))))))

        fun user(visibility: String) = Me("u1", "alice", "password", preferences = prefs(visibility))
    }
}
