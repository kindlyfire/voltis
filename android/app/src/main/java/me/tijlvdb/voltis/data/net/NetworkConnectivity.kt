package me.tijlvdb.voltis.data.net

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import me.tijlvdb.voltis.data.api.ConnectivityProbe
import me.tijlvdb.voltis.data.api.ServerUrl
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.auth.AppScope
import me.tijlvdb.voltis.data.auth.SessionState
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.domain.net.Connectivity

/**
 * Whether the current server answers (P2 §10): a probe of `/api/info`, the reports of every request
 * ([ReachabilityInterceptor]) and the device's default network.
 *
 * - Losing the network turns offline after [GRACE_MS], so a handover from Wi-Fi to mobile data doesn't
 *   flip it twice; a network coming back probes. A server on this device ([loopback]) ignores the network.
 * - A request that gets no genuine answer probes while online; a failed probe is offline, unless a genuine
 *   answer arrived while it ran. A passing probe or any genuine answer is online.
 * - It probes on coming to the front and when the current server changes. Offline in the foreground with
 *   a network, it probes again after 5, 10, 20, 40, then every 60 s.
 * - [checking] lasts from the start until the first probe has answered; one still running after [CHECK_MS]
 *   counts as failed.
 */
@Singleton
class NetworkConnectivity internal constructor(
    private val scope: CoroutineScope,
    /** The probe's request: whether the current server answered as itself; null without a server, or after it changed. */
    private val check: suspend () -> Boolean?,
    /** The current server is on this device. */
    private val loopback: suspend () -> Boolean,
    /** The current server's ID, once loaded. */
    servers: Flow<String?> = emptyFlow(),
    private val context: Context? = null,
    /** Milliseconds on a monotonic clock. */
    private val now: () -> Long = { System.nanoTime() / 1_000_000 },
) : Connectivity {
    @Inject
    constructor(api: VoltisApi, session: SessionStore, @AppScope scope: CoroutineScope, @ApplicationContext context: Context) :
        this(
            scope,
            probeOf(api, session),
            { session.loaded()?.server?.url?.host?.let(ServerUrl::isLoopback) == true },
            session.state.filter { it != SessionState.Loading }.map { it.server?.id },
            context,
        )

    private val state = MutableStateFlow(true)
    override val online: StateFlow<Boolean> = state.asStateFlow()

    private val _checking = MutableStateFlow(true)
    override val checking: StateFlow<Boolean> = _checking.asStateFlow()

    private var running: Deferred<Boolean>? = null
    private var network = true
    /** The network callback has reported, so [start]'s reading is older. */
    private var heard = false
    private var started = false
    private var foreground = false
    /** The app has left the front since the process started. */
    private var wentBack = false
    /** When the last probe started, on [now]'s clock. */
    private var probeStarted = Long.MIN_VALUE / 2
    private var grace: Job? = null
    private var schedule: Job? = null
    /** Counts genuine answers: a probe that failed meanwhile doesn't turn offline. */
    private val answers = AtomicLong()

    init {
        // A backstop for a start that never comes; [start] ends checking itself.
        scope.launch {
            delay(CHECK_MS)
            if (synchronized(this@NetworkConnectivity) { !started }) _checking.value = false
        }
        scope.launch {
            servers.distinctUntilChanged().drop(1).collect {
                // A probe still running is the previous server's, and would change nothing.
                synchronized(this@NetworkConnectivity) { running }?.join()
                probe()
            }
        }
    }

    /** Watches the default network and checks the server once. Called once, at process start. */
    fun begin() {
        val manager = context?.getSystemService(ConnectivityManager::class.java) ?: return start(true)
        manager.registerDefaultNetworkCallback(
            object : ConnectivityManager.NetworkCallback() {
                override fun onAvailable(network: Network) = networkChanged(true)

                override fun onLost(network: Network) = networkChanged(false)
            },
        )
        // Read after registering, so a network arriving in between isn't missed.
        start(manager.activeNetwork != null)
    }

    /** The first check: a probe that decides within [CHECK_MS], or offline at once without a network. */
    internal fun start(hasNetwork: Boolean) {
        val present = synchronized(this) {
            started = true
            if (!heard) network = hasNetwork
            network
        }
        scope.launch {
            if (!present && !loopback()) {
                state.value = false
            } else {
                val seen = answers.get()
                if (withTimeoutOrNull(CHECK_MS) { probe() } == null) failed(seen)
            }
            _checking.value = false
        }
    }

    /** The default network appeared (or changed) or went. */
    internal fun networkChanged(available: Boolean) {
        synchronized(this) {
            heard = true
            grace?.cancel()
            grace = null
            if (available) {
                network = true
            } else {
                grace = scope.launch {
                    delay(GRACE_MS)
                    lost()
                }
                return
            }
        }
        scope.launch { probe() }
    }

    private suspend fun lost() {
        if (loopback()) return
        val job = currentCoroutineContext()[Job]
        synchronized(this) {
            // The network came back since the grace ran out.
            if (grace !== job) return
            network = false
            schedule?.cancel()
            schedule = null
            state.value = false
        }
    }

    /** The app came to the front or went: the schedule probes only in front. */
    fun foreground(inFront: Boolean) {
        val again = synchronized(this) {
            foreground = inFront
            if (!inFront) {
                wentBack = true
                schedule?.cancel()
                schedule = null
            }
            // Only a probe in flight or just made stands in for this one: a process a worker started may have probed long ago.
            val fresh = running?.isActive == true || now() - probeStarted < FRESH_MS
            inFront && (wentBack || !state.value || !fresh)
        }
        if (again) scope.launch { probe() }
    }

    /** One at a time: a caller arriving while a probe runs gets its result. Without a server it fails and changes nothing. */
    override suspend fun probe(): Boolean {
        val probe = synchronized(this) {
            running?.takeIf { it.isActive } ?: scope.async { run() }.also { running = it }
        }
        return probe.await()
    }

    private suspend fun run(): Boolean {
        synchronized(this) { probeStarted = now() }
        val seen = answers.get()
        val passed = try {
            check()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            false
        }
        if (passed == true) {
            state.value = true
            stopRetries()
        } else if (passed == false) {
            failed(seen)
        }
        _checking.value = false
        return passed == true
    }

    /** A probe failed: offline, unless a genuine answer came after [seen]. */
    private suspend fun failed(seen: Long) {
        synchronized(this) {
            if (answers.get() != seen) return
            state.value = false
        }
        scheduleRetries()
    }

    override fun answered() {
        synchronized(this) {
            answers.incrementAndGet()
            state.value = true
        }
        _checking.value = false
        stopRetries()
    }

    /** Offline, the schedule probes instead. */
    override fun unreachable() {
        if (state.value) scope.launch { probe() }
    }

    private fun stopRetries() = synchronized(this) {
        schedule?.cancel()
        schedule = null
    }

    /** Starts the probes' schedule unless it runs, or there is nothing it could reach. */
    private suspend fun scheduleRetries() {
        val reachable = synchronized(this) { network } || loopback()
        synchronized(this) {
            if (!foreground || !reachable || schedule?.isActive == true) return
            schedule = scope.launch {
                var wait = FIRST_RETRY_MS
                while (true) {
                    delay(wait)
                    wait = (wait * 2).coerceAtMost(LAST_RETRY_MS)
                    // From inside the schedule: a passing probe stops it, and a failing one leaves it running.
                    scope.launch { probe() }.join()
                }
            }
        }
    }

    internal companion object {
        const val GRACE_MS = 3_000L
        const val CHECK_MS = 3_000L
        const val FRESH_MS = 5_000L
        const val FIRST_RETRY_MS = 5_000L
        const val LAST_RETRY_MS = 60_000L
        private const val TIMEOUT_SECONDS = 3
        /** The whole call, DNS included, which the connect timeout doesn't cover. */
        private const val CALL_TIMEOUT_SECONDS = 5L

        /** Blocking on the IO pool: [AuthInterceptor] may wait for it in a dispatcher slot of the server's host. */
        fun probeOf(api: VoltisApi, session: SessionStore): suspend () -> Boolean? = probe@{
            val server = session.loaded()?.server ?: return@probe null
            val passed = try {
                withContext(Dispatchers.IO) {
                    api.probe(ConnectivityProbe, TIMEOUT_SECONDS, TIMEOUT_SECONDS).apply { timeout().timeout(CALL_TIMEOUT_SECONDS, TimeUnit.SECONDS) }.execute()
                }.body()?.serverId == server.id
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                // No answer, or one that isn't Voltis' info.
                false
            }
            // Signed in to another server meanwhile: this answer says nothing about it.
            passed.takeIf { session.active()?.server?.id == server.id }
        }

        private suspend fun SessionStore.loaded(): SessionStore.Active? {
            state.first { it != SessionState.Loading }
            return active()
        }
    }
}
