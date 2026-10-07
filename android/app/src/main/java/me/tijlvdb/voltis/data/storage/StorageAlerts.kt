package me.tijlvdb.voltis.data.storage

import android.os.SystemClock
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.asSharedFlow

/**
 * "Storage is full" for the first [full] call after [QUIET_MS] without one: a disk that stays full gives
 * one snackbar, not one per page turn or per engine retry. Process-wide, like the disk.
 */
object StorageAlerts {
    const val QUIET_MS = 30_000L

    private val _full = MutableSharedFlow<Unit>(extraBufferCapacity = 1, onBufferOverflow = BufferOverflow.DROP_OLDEST)
    val full: SharedFlow<Unit> = _full.asSharedFlow()

    private var last: Long? = null
    internal var clock: () -> Long = { SystemClock.elapsedRealtime() }

    @Synchronized
    fun full() {
        val now = clock()
        // Measured from the previous call, not the previous alert: a disk that stays full keeps failing, and stays quiet.
        val quiet = last?.let { now - it <= QUIET_MS } == true
        last = now
        if (!quiet) _full.tryEmit(Unit)
    }

    internal fun reset() {
        last = null
        clock = { SystemClock.elapsedRealtime() }
    }
}
