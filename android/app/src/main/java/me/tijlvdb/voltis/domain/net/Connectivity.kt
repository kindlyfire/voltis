package me.tijlvdb.voltis.domain.net

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.first

/** Whether the server can be reached (P2 §10). Only a genuine Voltis answer counts as reaching it. */
interface Connectivity {
    val online: StateFlow<Boolean>

    /** From process start until the first probe has answered, at most a few seconds; [online] is true meanwhile. */
    val checking: StateFlow<Boolean> get() = Settled

    /** `GET /api/info` with short timeouts; passes only when its `server_id` is the stored server's. */
    suspend fun probe(): Boolean

    /** A request to the server got a genuine answer. */
    fun answered()

    /** A request to the server got none. */
    fun unreachable()
}

private val Settled: StateFlow<Boolean> = MutableStateFlow(false)

/** Waits out the start's check: a screen that loads from the server then knows whether to try. */
suspend fun Connectivity.settled() {
    checking.first { !it }
}
