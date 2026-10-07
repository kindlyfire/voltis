package me.tijlvdb.voltis.data.net

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import me.tijlvdb.voltis.data.api.ConnectivityProbe
import me.tijlvdb.voltis.data.api.PLACEHOLDER_HOST
import me.tijlvdb.voltis.data.api.ServerUrlInterceptor
import me.tijlvdb.voltis.data.api.testApi
import me.tijlvdb.voltis.data.auth.testStore
import me.tijlvdb.voltis.domain.net.Connectivity
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import okhttp3.Headers.Companion.headersOf
import okhttp3.OkHttpClient
import okhttp3.Request
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

@OptIn(ExperimentalCoroutinesApi::class)
class ConnectivityTest {
    @get:Rule
    val tmp = TemporaryFolder()

    /** Only [probeAndReports] needs one; the state tests run on the [Rig]. */
    private val serverStarted = lazy { MockWebServer().also { it.start() } }
    private val server by serverStarted

    @After
    fun stop() {
        if (serverStarted.isInitialized()) server.close()
    }

    /** The probe answers [reachable] after [latency]; [probes] records the virtual time each started. */
    private class Rig(scope: TestScope, var reachable: Boolean = true, var loopback: Boolean = false, var latency: Long = 0) {
        val probes = mutableListOf<Long>()
        val servers = MutableStateFlow<String?>("a")
        val connectivity = NetworkConnectivity(
            scope.backgroundScope,
            {
                probes += scope.testScheduler.currentTime
                delay(latency)
                reachable
            },
            { loopback },
            servers,
            now = { scope.testScheduler.currentTime },
        )
    }

    @Test
    fun theFirstForegroundProbesAgainWhenTheStartupProbeIsOld() = runTest {
        val rig = Rig(this)
        rig.connectivity.start(hasNetwork = true)
        runCurrent()
        assertEquals(1, rig.probes.size)
        // Just made: it stands in.
        rig.connectivity.foreground(true)
        runCurrent()
        assertEquals(1, rig.probes.size)
        rig.connectivity.foreground(false)
        // A process a worker started: no foreground yet, the probe is old and the server is gone.
        val late = Rig(this)
        late.connectivity.start(hasNetwork = true)
        runCurrent()
        advanceTimeBy(NetworkConnectivity.FRESH_MS + 1)
        late.reachable = false
        late.connectivity.foreground(true)
        runCurrent()
        assertEquals(2, late.probes.size)
        assertEquals(false, late.connectivity.online.value)
    }

    @Test
    fun stateInVirtualTime() = runTest {
        // Checking ends at the first genuine answer, or once the first probe decided; one still running
        // after CHECK_MS counts as failed, and its late answer still counts.
        val early = Rig(this).connectivity
        early.answered()
        assertEquals(false, early.checking.value)
        val slow = Rig(this, latency = NetworkConnectivity.CHECK_MS + 1_000)
        slow.connectivity.start(hasNetwork = true)
        advanceTimeBy(NetworkConnectivity.CHECK_MS - 1)
        assertEquals(true, slow.connectivity.checking.value)
        advanceTimeBy(1)
        runCurrent()
        assertEquals(listOf(false, false), listOf(slow.connectivity.checking.value, slow.connectivity.online.value))
        advanceTimeBy(1_000)
        runCurrent()
        assertEquals(true, slow.connectivity.online.value)

        val rig = Rig(this, reachable = false)
        val c = rig.connectivity
        val t = testScheduler.currentTime
        assertEquals(listOf(true, true), listOf(c.checking.value, c.online.value))
        c.start(hasNetwork = true)
        runCurrent()
        assertEquals(listOf(false, false), listOf(c.checking.value, c.online.value))

        // Coming to the front probes; offline in front with a network, again after 5, 10, 20, 40, then every 60 s.
        c.foreground(true)
        advanceTimeBy(5_000 + 10_000 + 20_000 + 40_000 + 60_000 + 60_000 + 1)
        assertEquals(listOf(0L, 0, 5_000, 15_000, 35_000, 75_000, 135_000, 195_000), rig.probes.map { it - t })
        // Leaving the front cancels the schedule; coming back probes at once, and a pass stops it.
        c.foreground(false)
        advanceTimeBy(600_000)
        assertEquals(8, rig.probes.size)
        rig.reachable = true
        c.foreground(true)
        runCurrent()
        assertEquals(listOf(true, 9), listOf(c.online.value, rig.probes.size))
        advanceTimeBy(600_000)
        assertEquals(9, rig.probes.size)

        // Losing the network turns offline only after the grace: a handover within it changes nothing.
        c.networkChanged(false)
        advanceTimeBy(2_000)
        c.networkChanged(true)
        runCurrent()
        assertEquals(listOf(true, 10), listOf(c.online.value, rig.probes.size))
        c.networkChanged(false)
        advanceTimeBy(NetworkConnectivity.GRACE_MS - 1)
        assertEquals(true, c.online.value)
        advanceTimeBy(1)
        runCurrent()
        assertEquals(false, c.online.value)
        // Without a network nothing is scheduled; a genuine answer is online at once.
        advanceTimeBy(600_000)
        assertEquals(10, rig.probes.size)
        c.answered()
        assertEquals(true, c.online.value)

        // In the background a failed probe schedules nothing, and offline a failed request doesn't probe.
        c.networkChanged(true)
        runCurrent()
        c.foreground(false)
        rig.reachable = false
        c.unreachable()
        advanceTimeBy(600_000)
        assertEquals(listOf(false, 12), listOf(c.online.value, rig.probes.size))
        c.unreachable()
        runCurrent()
        assertEquals(12, rig.probes.size)

        // A genuine answer while a probe fails keeps it online.
        c.answered()
        rig.latency = 1_000
        c.unreachable()
        advanceTimeBy(500)
        c.answered()
        advanceTimeBy(1_000)
        assertEquals(listOf(true, 13), listOf(c.online.value, rig.probes.size))

        // Switching servers probes the new one.
        rig.servers.value = "b"
        advanceTimeBy(1_001)
        assertEquals(listOf(false, 14), listOf(c.online.value, rig.probes.size))
    }

    @Test
    fun loopbackIgnoresTheNetwork() = runTest {
        val rig = Rig(this, loopback = true)
        val c = rig.connectivity
        // Started without a network, a server on this device is still probed; losing the network changes nothing.
        c.start(hasNetwork = false)
        runCurrent()
        c.networkChanged(false)
        advanceTimeBy(10_000)
        assertEquals(listOf(true, 1), listOf(c.online.value, rig.probes.size))

        // Elsewhere, a start without a network is offline at once, with no probe; the check ends after at most 3 s anyway.
        val remote = Rig(this)
        assertEquals(true, remote.connectivity.checking.value)
        remote.connectivity.start(hasNetwork = false)
        runCurrent()
        assertEquals(listOf(false, false, 0), listOf(remote.connectivity.online.value, remote.connectivity.checking.value, remote.probes.size))
        val idle = Rig(this)
        advanceTimeBy(NetworkConnectivity.CHECK_MS + 1)
        assertEquals(false, idle.connectivity.checking.value)
    }

    /** The real probe and the interceptor's reports, against a server. */
    @Test(timeout = 10_000)
    fun probeAndReports() = runTest {
        val store = testStore(tmp.newFolder().resolve("s.preferences_pb"))
        store.connect("srv", server.url("/"))
        val json = headersOf("Content-Type", "application/json")
        val probe = NetworkConnectivity.probeOf(testApi(server.url("/")), store)
        fun info(id: String) = """{"server_id": "$id", "api_version": 1, "first_user_flow": false, "password_login_enabled": true, "oidc_enabled": false, "oidc_button_label": ""}"""
        server.enqueue(MockResponse(200, json, info("srv")))
        server.enqueue(MockResponse(200, json, info("other")))
        server.enqueue(MockResponse(200, headersOf("Content-Type", "text/html"), "<html>"))
        assertEquals(listOf(true, false, false), listOf(probe(), probe(), probe()))

        val reports = mutableListOf<String>()
        val connectivity = object : Connectivity {
            override val online = MutableStateFlow(true)

            override suspend fun probe() = true

            override fun answered() {
                reports += "answered"
            }

            override fun unreachable() {
                reports += "unreachable"
            }
        }
        val client = OkHttpClient.Builder()
            .addInterceptor(ServerUrlInterceptor(store))
            .addInterceptor(ReachabilityInterceptor(store) { connectivity })
            .build()
        fun get(url: String, probe: Boolean = false) = runCatching {
            client.newCall(Request.Builder().url(url).apply { if (probe) tag(ConnectivityProbe::class.java, ConnectivityProbe) }.build()).execute().close()
        }
        server.enqueue(MockResponse(404, json, """{"error": "Content not found"}"""))
        server.enqueue(MockResponse(200, headersOf("Content-Type", "text/html"), "<html>"))
        server.enqueue(MockResponse(502, json, """{"error": "Bad gateway"}"""))
        server.enqueue(MockResponse(502, json, """{"error": "Bad gateway"}"""))
        get("http://$PLACEHOLDER_HOST/api/content/c_1")
        get("http://$PLACEHOLDER_HOST/api/content/c_1")
        get("http://$PLACEHOLDER_HOST/api/content/c_1")
        // The probe reports itself, and another origin isn't the server.
        get("http://$PLACEHOLDER_HOST/api/info", probe = true)
        get("http://127.0.0.1:1/api/info")
        assertEquals(listOf("answered", "unreachable", "unreachable"), reports)
    }
}
