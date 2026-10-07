package me.tijlvdb.voltis.data.reading

import androidx.datastore.core.DataStore
import androidx.datastore.core.handlers.ReplaceFileCorruptionHandler
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.emptyPreferences
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey
import android.util.Log
import java.io.File
import java.io.IOException
import java.security.SecureRandom
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import me.tijlvdb.voltis.data.storage.localWrite
import me.tijlvdb.voltis.domain.storage.StorageFullException

/** The writer fields of the engine's requests: [WriterIdentity], or a fake in tests. */
interface ReadingWriter {
    val writerId: String

    /** Suspends while the next `seq` isn't reserved yet; throws an [IOException], or a [me.tijlvdb.voltis.domain.storage.StorageFullException] on a full disk, when none can be. */
    suspend fun ensureReserved()

    /** The next `seq`; [ensureReserved] first. */
    fun nextSeq(): Long
}

/**
 * The install's reader identity: a `writer_id` and a `seq` that never decreases and is never
 * reused (P2 §4). `seq` is handed out from blocks reserved on disk ahead of use, so a crash loses at
 * most the rest of a block.
 */
class WriterIdentity(private val store: DataStore<Preferences>, private val scope: CoroutineScope) : ReadingWriter {
    private val lock = Mutex()
    private var id: String? = null
    private var next = 1L

    /** The highest `seq` stored as reserved. */
    private var limit = 0L
    private var reserving: Job? = null

    /** Why the last reservation failed; null after one that succeeded. [ensureReserved] rethrows it. */
    @Volatile
    private var failure: Exception? = null

    override val writerId get() = checkNotNull(id) { "load() first" }

    /**
     * Reads the identity and reserves the first block. Once, before any `seq` is handed out. On a full
     * disk the identity is read without the block: nothing can be sent until [ensureReserved] gets one, and
     * reading is not held up for it.
     */
    suspend fun load() {
        lock.withLock {
            var writer = ""
            var reserved = 0L
            var block = BLOCK
            try {
                localWrite {
                    store.edit { prefs ->
                        writer = prefs[WRITER_ID] ?: newWriterId().also { prefs[WRITER_ID] = it }
                        reserved = prefs[RESERVED] ?: 0L
                        prefs[RESERVED] = reserved + BLOCK
                    }
                }
            } catch (e: StorageFullException) {
                Log.w("WriterIdentity", "Couldn't reserve the first seq block: storage is full", e)
                val prefs = store.data.first()
                writer = prefs[WRITER_ID] ?: newWriterId()
                reserved = prefs[RESERVED] ?: 0L
                block = 0L
                failure = e
            }
            synchronized(this) {
                id = writer
                next = reserved + 1
                limit = reserved + block
            }
        }
    }

    /** The next `seq`. Call [ensureReserved] first: there must be one left. */
    @Synchronized
    override fun nextSeq(): Long {
        check(next <= limit) { "No seq reserved" }
        val seq = next++
        if (limit - next + 1 < LOW) reserve()
        return seq
    }

    /**
     * Suspends only while the next block is still being written and none is left. Throws an
     * [IOException] when the `writer` file can't be written, after one more try.
     */
    override suspend fun ensureReserved() {
        repeat(2) {
            val pending = synchronized(this) { if (next <= limit) return else reserve() }
            pending.join()
        }
        synchronized(this) {
            if (next > limit) {
                val cause = failure
                if (cause is StorageFullException) throw StorageFullException(cause)
                throw IOException("Couldn't reserve a seq", cause)
            }
        }
    }

    @Synchronized
    private fun reserve(): Job = reserving?.takeIf { it.isActive } ?: scope.launch {
        lock.withLock {
            var reserved = 0L
            try {
                localWrite {
                    store.edit { prefs ->
                        reserved = maxOf(prefs[RESERVED] ?: 0L, limit) + BLOCK
                        prefs[RESERVED] = reserved
                    }
                }
            } catch (e: IOException) {
                // ensureReserved() tries again, and reports it.
                Log.w("WriterIdentity", "Couldn't reserve a seq block", e)
                failure = e
                return@withLock
            } catch (e: StorageFullException) {
                Log.w("WriterIdentity", "Couldn't reserve a seq block: storage is full", e)
                failure = e
                return@withLock
            }
            failure = null
            // Only once it is on disk.
            synchronized(this@WriterIdentity) { limit = reserved }
        }
    }.also { reserving = it }

    companion object {
        const val BLOCK = 64L
        private const val LOW = 16L
        private val WRITER_ID = stringPreferencesKey("writer_id")
        private val RESERVED = longPreferencesKey("seq_reserved")

        /** `t` and 16 of `[0-9a-z]`, as the server's `writerIDPattern` requires. */
        fun newWriterId(): String {
            val random = SecureRandom()
            return "t" + (1..16).joinToString("") { random.nextInt(36).toString(36) }
        }

        /** A corrupt file starts over empty, so [load] makes a new `writer_id`: never a low `seq` under the old one. */
        fun dataStore(scope: CoroutineScope, file: () -> File): DataStore<Preferences> = PreferenceDataStoreFactory.create(
            corruptionHandler = ReplaceFileCorruptionHandler { emptyPreferences() },
            scope = scope,
            produceFile = file,
        )
    }
}
