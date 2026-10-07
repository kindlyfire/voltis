package me.tijlvdb.voltis.data.storage

import android.os.StatFs
import java.io.File
import me.tijlvdb.voltis.domain.storage.StorageSpace

object AndroidStorageSpace : StorageSpace {
    override fun free(dir: File): Long = try {
        StatFs(dir.path).availableBytes
    } catch (_: IllegalArgumentException) {
        // A directory that doesn't exist (yet): the answer is unknown, not empty.
        Long.MAX_VALUE
    }
}
