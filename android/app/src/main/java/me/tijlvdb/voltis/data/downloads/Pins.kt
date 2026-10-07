package me.tijlvdb.voltis.data.downloads

import java.io.File

/** A copy directory of one account: [accountDir] is the account directory's name. */
data class PinKey(val accountDir: String, val dirId: String)

/** Process-wide reader pins, so they survive an owner restart for the same account. Counts only, no I/O. */
internal object Pins {
    private val counts = HashMap<PinKey, Int>()

    /** Only under an owner's lock. */
    fun add(key: PinKey) = synchronized(counts) { counts[key] = (counts[key] ?: 0) + 1 }

    /** True when the count reached 0. Any thread. */
    fun remove(key: PinKey): Boolean = synchronized(counts) {
        val left = (counts[key] ?: 1) - 1
        if (left > 0) counts[key] = left else counts.remove(key)
        left <= 0
    }

    fun pinned(key: PinKey): Boolean = synchronized(counts) { key in counts }
}

/**
 * One reader's claim on one copy, holding one [Pins] unit while its own count is above 0: a root
 * hold, released when its owner Job completes, plus child holds. Non-suspending, no I/O.
 */
class CopyPin internal constructor(
    val copyId: String,
    val dir: File,
    val stale: String?,
    /** The copy's page count and total bytes as published, for the files' length check. */
    val pageCount: Int,
    val bytes: Long,
    private val key: PinKey,
    private val onFree: (PinKey) -> Unit,
) {
    private var count = 1
    private var root = true

    /** Keeps the files until closed; null once the pin is free (it never comes back). */
    fun hold(): AutoCloseable? {
        synchronized(this) {
            if (count == 0) return null
            count++
        }
        var closed = false
        return AutoCloseable {
            if (synchronized(this) { !closed.also { closed = true } }) drop()
        }
    }

    /** Drops the root hold early. Idempotent. */
    fun release() {
        if (synchronized(this) { root.also { root = false } }) drop()
    }

    private fun drop() {
        if (synchronized(this) { --count == 0 } && Pins.remove(key)) onFree(key)
    }
}
