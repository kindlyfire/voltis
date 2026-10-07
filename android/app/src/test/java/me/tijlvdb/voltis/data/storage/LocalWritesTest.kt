package me.tijlvdb.voltis.data.storage

import android.app.Application
import android.database.sqlite.SQLiteFullException
import android.system.ErrnoException
import android.system.OsConstants
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.File
import java.io.IOException
import kotlinx.coroutines.runBlocking
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.domain.storage.LOW_SPACE
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.domain.storage.StorageSpace
import org.junit.After
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** A local write that fails for lack of space is a [StorageFullException] with an alert, and nothing else is. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [37], application = Application::class)
class LocalWritesTest {
    private val db = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), VoltisDatabase::class.java).build()

    @After
    fun close() {
        db.close()
    }

    private class Space(var free: Long) : StorageSpace {
        override fun free(dir: File) = free
    }

    private fun noSpace() = IOException("Inoperable file", IOException("write failed", ErrnoException("write", OsConstants.ENOSPC)))

    @Test
    fun whatIsAFullDisk() = runBlocking {
        val plenty = Space(100L shl 20)
        val none = Space(0)
        suspend fun failure(space: StorageSpace, error: Throwable) = try {
            localWrite(space = space) { throw error }
        } catch (e: Exception) {
            e
        }
        // SQLite's own answer, at the COMMIT.
        assertTrue(runCatching { db.localTransaction { throw SQLiteFullException("database or disk is full") } }.exceptionOrNull() is StorageFullException)
        // ENOSPC anywhere in the causes, or less than LOW_SPACE free.
        assertTrue(failure(plenty, noSpace()) is StorageFullException)
        assertTrue(failure(none, IOException("Inoperable file")) is StorageFullException)
        assertTrue(failure(Space(LOW_SPACE - 1), IOException("x")) is StorageFullException)
        // Space, and another cause: as it is.
        val other = IOException("Permission denied")
        // (Coroutines may hand back a copy of the exception: its class and message are what count.)
        fun same(e: Throwable?) = e is IOException && e !is StorageFullException && e.message == "Permission denied"
        assertTrue(same(failure(plenty, other)))
        assertTrue(same(runCatching { db.localTransaction { throw other } }.exceptionOrNull()))
    }
}
