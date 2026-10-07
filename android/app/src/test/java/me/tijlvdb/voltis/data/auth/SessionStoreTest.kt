package me.tijlvdb.voltis.data.auth

import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import java.io.File
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.emitAll
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.job
import kotlinx.coroutines.test.runTest
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class SessionStoreTest {
    @get:Rule
    val tmp = TemporaryFolder()

    /** A sign-out applies to the session it was asked for only; its delete intent is stored with it and cleared by a sign-in. */
    @Test
    fun aSignOutNeverClearsANewerSession() = runTest {
        val file = File(tmp.root, "session.preferences_pb")
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        val store = testStore(file, scope)
        store.connect("srv", "https://voltis.example".toHttpUrl())
        store.signIn("srv", "t1", "u1", "alice")
        val old = store.active()!!
        // Signed in again, as the same user, while the old sign-out was on its way.
        store.signIn("srv", "t2", "u1", "alice")
        assertFalse(store.signOut(old, deleteIntent = "srv/u1"))
        assertEquals("t2", store.active()?.token)
        assertNull(store.pending())

        // The new session ended by a 401 meanwhile: the old sign-out still isn't for it.
        store.markNeedsReauth(store.active()!!)
        assertFalse(store.signOut(old, deleteIntent = "srv/u1"))
        store.signIn("srv", "t2", "u1", "alice")
        assertTrue(store.signOut(store.active()!!, deleteIntent = "srv/u1"))
        scope.coroutineContext.job.cancelAndJoin()
        val reopened = testStore(file)
        reopened.state.first { it != SessionState.Loading }
        assertEquals("srv/u1", reopened.pending())
        assertTrue(reopened.isPending("srv/u1"))
        reopened.signIn("srv", "t3", "u1", "alice")
        assertNull(reopened.pending())
        assertFalse(reopened.isPending("srv/u1"))
    }

    /** A read that fails is not an empty session: nothing is cached, and the next change reads the file again, keeping what it held. */
    /** The session and its generation are one read: a sign-out captured while a replacement's write is in flight can't pass for the replacement. */
    @Test
    fun aSignOutCapturedDuringAReplacementSignInIsRejected() = runTest {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        val real = PreferenceDataStoreFactory.create(scope = scope) { File(tmp.root, "gated.preferences_pb") }
        var gate: CompletableDeferred<Unit>? = null
        val paused = CompletableDeferred<Unit>()
        val gated = object : DataStore<Preferences> {
            override val data = real.data

            override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences {
                gate?.let { paused.complete(Unit); it.await() }
                return real.updateData(transform)
            }
        }
        val store = SessionStore(gated, FakeCipher, scope)
        store.connect("srv", "https://voltis.example".toHttpUrl())
        store.signIn("srv", "t1", "u1", "alice")

        gate = CompletableDeferred()
        val replacement = async(Dispatchers.IO) { store.signIn("srv", "t2", "u1", "alice") }
        paused.await()
        val captured = store.active()!!
        assertEquals("t1", captured.token)
        gate.complete(Unit)
        replacement.await()
        // A 401 of the replacement leaves its record tokenless.
        store.markNeedsReauth(store.active()!!)
        assertFalse(store.signOut(captured, deleteIntent = "srv/u1"))
        assertNull(store.pending())
        assertEquals(SessionState.NeedsReauth(Server("srv", "https://voltis.example".toHttpUrl()), "u1"), store.state.value)
    }

    @Test
    fun anUnreadableSessionIsNotAnEmptyOne() = runTest {
        val file = File(tmp.root, "session.preferences_pb")
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        val first = testStore(file, scope)
        val home = "https://voltis.example".toHttpUrl()
        first.connect("srv", home)
        first.signIn("srv", "t1", "u1", "alice")
        first.connect("srv2", "https://other.example".toHttpUrl())
        first.signIn("srv2", "t2", "u2", "bob")
        first.signOut(first.active()!!, deleteIntent = "srv2/u2")
        first.connect("srv", home)
        scope.coroutineContext.job.cancelAndJoin()

        val scope2 = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        val real = PreferenceDataStoreFactory.create(scope = scope2) { file }
        var failing = true
        val flaky = object : DataStore<Preferences> {
            override val data = flow {
                if (failing) {
                    failing = false
                    throw IllegalStateException("one-off")
                }
                emitAll(real.data)
            }

            override suspend fun updateData(transform: suspend (Preferences) -> Preferences) = real.updateData(transform)
        }
        val store = SessionStore(flaky, FakeCipher, scope2)
        assertEquals(SessionState.NoServer, store.state.first { it != SessionState.Loading })
        assertNull(store.active())

        // The next change reads the file, then applies: the token, the other record and the intent are all still there.
        store.connect("srv", home)
        assertEquals(SessionState.SignedIn(Server("srv", home), "u1", "alice"), store.state.value)
        assertEquals("srv2/u2", store.pending())
        assertEquals("https://other.example/", store.server("srv2")?.url?.toString())
    }

    @Test
    fun serverIdentityAndSignIn() = runTest {
        val file = File(tmp.root, "session.preferences_pb")
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        val store = testStore(file, scope)
        val home = "https://voltis.example".toHttpUrl()

        store.connect("srv", home)
        store.signIn("srv", "t1", "u1", "alice")
        assertEquals(SessionState.SignedIn(Server("srv", home), "u1", "alice"), store.state.value)

        // Same origin, another path: the token stays.
        val sub = "https://voltis.example/sub".toHttpUrl()
        store.connect("srv", sub)
        assertEquals("t1", store.active()?.token)
        assertEquals(SessionState.SignedIn(Server("srv", sub), "u1", "alice"), store.state.value)

        // Same server_id from another origin: no token for it, but the user is kept.
        val lan = "http://192.168.1.5:8080".toHttpUrl()
        store.connect("srv", lan)
        assertNull(store.active()?.token)
        assertEquals(SessionState.NeedsReauth(Server("srv", lan), "u1"), store.state.value)

        // Signing in as someone else replaces the user.
        store.signIn("srv", "t2", "u2", "bob")
        assertEquals(SessionState.SignedIn(Server("srv", lan), "u2", "bob"), store.state.value)

        // The pending flow survives a reload and is taken once.
        store.saveFlow(PendingFlow("verifier", "srv", 1))
        scope.coroutineContext.job.cancelAndJoin()

        val reopened = testStore(file)
        assertEquals(SessionState.SignedIn(Server("srv", lan), "u2", "bob"), reopened.state.first { it != SessionState.Loading })
        assertEquals("t2", reopened.active()?.token)
        assertEquals(PendingFlow("verifier", "srv", 1), reopened.takeFlow())
        assertNull(reopened.takeFlow())
    }
}
