package me.tijlvdb.voltis.data.reading

import android.app.Application
import android.system.ErrnoException
import android.system.OsConstants
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.longPreferencesKey
import java.io.IOException
import java.io.File
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.job
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import me.tijlvdb.voltis.domain.storage.StorageFullException

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [37], application = Application::class)
class WriterIdentityTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private val file get() = File(tmp.root, "writer.preferences_pb")

    /** An identity on [file] with its own scope: one DataStore per file at a time, so close before reopening. */
    private class Opened(file: File) {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        val store = WriterIdentity.dataStore(scope) { file }
        val identity = WriterIdentity(store, scope)

        suspend fun reserved() = store.data.first()[longPreferencesKey("seq_reserved")] ?: 0L

        /** Hands out [n] seqs as the pump does, each checked against the mark on disk. */
        suspend fun take(n: Int) = List(n) {
            identity.ensureReserved()
            identity.nextSeq().also { assertTrue("$it above the stored mark", it <= reserved()) }
        }

        suspend fun close() = scope.coroutineContext.job.cancelAndJoin()
    }

    @Test(timeout = 20_000)
    fun seqNeverRepeatsOrDecreases() = runBlocking {
        val first = Opened(file)
        first.identity.load()
        val id = first.identity.writerId
        assertTrue(id, Regex("t[0-9a-z]{16}").matches(id))
        // Past two block boundaries.
        val before = first.take(2 * WriterIdentity.BLOCK.toInt() + 5)
        assertEquals(before.sorted().distinct(), before)
        assertEquals(1L, before.first())
        first.close()

        // A restart continues above everything reserved, under the same ID.
        val second = Opened(file)
        second.identity.load()
        assertEquals(id, second.identity.writerId)
        val after = second.take(3)
        assertTrue(after.first() > before.last())
        assertEquals(after.sorted().distinct(), after)
        second.close()

        // A corrupt file must not hand out a low seq under the old ID.
        file.writeBytes(byteArrayOf(1, 2, 3, 4, 5))
        val third = Opened(file)
        third.identity.load()
        assertNotEquals(id, third.identity.writerId)
        assertEquals(listOf(1L), third.take(1))
        third.close()
    }

    /** A store that can be made full: every write then fails as DataStore does on a full disk. */
    private class Fullable(private val inner: DataStore<Preferences>) : DataStore<Preferences> {
        @Volatile var full = false

        override val data get() = inner.data

        override suspend fun updateData(transform: suspend (Preferences) -> Preferences): Preferences {
            if (full) throw IOException("Inoperable file", IOException("write failed", ErrnoException("write", OsConstants.ENOSPC)))
            return inner.updateData(transform)
        }
    }

    @Test(timeout = 20_000)
    fun aFullDiskHandsOutNoSeqAndEscapesNothing() = runBlocking {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        val store = Fullable(WriterIdentity.dataStore(scope) { file })
        val identity = WriterIdentity(store, scope)
        identity.load()
        val id = identity.writerId
        // The disk fills before the block runs low, so the reservation ahead of use fails too (it would otherwise race
        // the fill), then the first block is used up: the next reservation is refused, and nothing leaks out of its launch.
        repeat(WriterIdentity.BLOCK.toInt() / 2) {
            identity.ensureReserved()
            identity.nextSeq()
        }
        store.full = true
        repeat(WriterIdentity.BLOCK.toInt() / 2) { identity.nextSeq() }
        assertTrue(runCatching { identity.ensureReserved() }.exceptionOrNull() is StorageFullException)
        // Space is back: a reservation goes through, above everything handed out.
        store.full = false
        identity.ensureReserved()
        assertEquals(WriterIdentity.BLOCK + 1, identity.nextSeq())
        scope.coroutineContext.job.cancelAndJoin()

        // A start on a full disk reads the identity without a block; seqs wait for one.
        val again = CoroutineScope(SupervisorJob() + Dispatchers.IO)
        val full = Fullable(WriterIdentity.dataStore(again) { file }).also { it.full = true }
        val restarted = WriterIdentity(full, again)
        restarted.load()
        assertEquals(id, restarted.writerId)
        assertTrue(runCatching { restarted.ensureReserved() }.exceptionOrNull() is StorageFullException)
        full.full = false
        restarted.ensureReserved()
        assertTrue(restarted.nextSeq() > 2 * WriterIdentity.BLOCK)
        again.coroutineContext.job.cancelAndJoin()
    }
}
