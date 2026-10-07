package me.tijlvdb.voltis.domain.storage

import java.io.File

/**
 * A local write failed for lack of space. Not an IOException: isUnreachable(), ListsRepository.unreachable
 * and toUiText() read those as the network.
 */
class StorageFullException(cause: Throwable? = null) : Exception("Storage is full", cause)

interface StorageSpace {
    /** `StatFs(dir).availableBytes`; never `File.freeSpace` or `usableSpace`, which count blocks reserved for root. */
    fun free(dir: File): Long
}

/** Below this much free space a failed local write is taken as a full disk. */
const val LOW_SPACE = 1L shl 20
