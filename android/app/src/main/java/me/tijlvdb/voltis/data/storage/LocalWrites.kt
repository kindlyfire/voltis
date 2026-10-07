package me.tijlvdb.voltis.data.storage

import android.database.sqlite.SQLiteFullException
import android.os.Environment
import android.system.ErrnoException
import android.system.OsConstants
import androidx.room.withTransaction
import java.io.File
import java.io.IOException
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.domain.storage.LOW_SPACE
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.domain.storage.StorageSpace

/**
 * [withTransaction] for a transaction that writes: a full disk (`SQLiteFullException`, thrown at the
 * `COMMIT`) is a [StorageFullException], with an alert.
 */
suspend fun <T> VoltisDatabase.localTransaction(block: suspend () -> T): T = try {
    withTransaction(block)
} catch (e: SQLiteFullException) {
    StorageAlerts.full()
    throw StorageFullException(e)
}

/**
 * A DataStore `edit` (or any file write) around [block]. An [IOException] is a [StorageFullException],
 * with an alert, when `ENOSPC` is in its causes or less than [LOW_SPACE] is free in [dir] (the data partition by default); any other
 * is rethrown. On a full disk DataStore throws "Inoperable file", caused by `ENOSPC`.
 */
suspend fun <T> localWrite(
    dir: () -> File = { Environment.getDataDirectory() },
    space: StorageSpace = AndroidStorageSpace,
    block: suspend () -> T,
): T = try {
    block()
} catch (e: IOException) {
    if (e.hasNoSpace() || space.free(dir()) < LOW_SPACE) {
        StorageAlerts.full()
        throw StorageFullException(e)
    }
    throw e
}

private fun Throwable.hasNoSpace(): Boolean = generateSequence(this) { it.cause?.takeIf { c -> c !== it } }.take(8).any {
    it is ErrnoException && it.errno == OsConstants.ENOSPC
}
