package me.tijlvdb.voltis.data.auth

import java.io.File
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.job
import kotlinx.coroutines.test.runTest
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class SessionStoreTest {
    @get:Rule
    val tmp = TemporaryFolder()

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
