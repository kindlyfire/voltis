package me.tijlvdb.voltis.data.downloads

import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.domain.downloads.DownloadError
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.ErrorKind
import me.tijlvdb.voltis.domain.downloads.MAX_ATTEMPTS
import me.tijlvdb.voltis.domain.downloads.ServerFile
import me.tijlvdb.voltis.domain.downloads.Stale
import me.tijlvdb.voltis.domain.downloads.staleOf

// The download row's transitions (P2 §8): one pure function, applied only by DownloadStore.

data class NewDownload(val contentId: String, val seriesId: String, val requestedBy: String)

/**
 * What a bulk download does to an existing [row] (P4 §6), by its state and its copy's staleness, not its badge:
 * Retry for a failed one even with a retained copy, "Download again" for a done stale or damaged copy.
 * Null: already present (queued, running or paused, a replacement included) or a current copy.
 */
fun bulkEvent(row: DownloadEntity): Event? = when {
    row.state == DownloadState.FAILED -> Event.Retry
    row.state == DownloadState.DONE && (row.stale == Stale.VERSION || row.stale == Stale.DAMAGED) -> Event.Again
    else -> null
}

/** What the server said of a download: [server] null, or not valid, is gone. [at] is when its request started (wall clock). */
data class Observation(val contentId: String, val server: ServerFile?, val at: Long)

/** What a transfer made of its directory before it became the copy. */
data class CopyMade(val bytes: Long, val cover: Boolean, val seriesCover: Boolean)

sealed interface Change {
    data object None : Change

    data class Write(val row: DownloadEntity) : Change

    data object Delete : Change
}

sealed interface Event {
    // Controls.
    data class Enqueue(val new: NewDownload) : Event

    /** Pause, and Pause all with no [expect]; a notification's carries the transfer it was posted for. */
    data class Pause(val expect: String? = null) : Event

    data object Resume : Event

    data object ResumeAll : Event

    data object Retry : Event

    data object Again : Event

    /** A reader found the files of copy [copyId] damaged. A former, pinned copy marks nothing. */
    data class Damaged(val copyId: String) : Event

    data class Cancel(val expect: String? = null) : Event

    /** The user asked for a volume that is completed: automatic deletes leave it. Sticky. */
    data object Keep : Event

    data object Delete : Event

    data class Observe(val server: ServerFile?, val at: Long) : Event

    // A run's events: the owner applies them only to the run it admits.
    data object Claim : Event

    data object Restarted : Event

    data class Manifest(val manifest: OfflineManifest) : Event

    data class Page(val index: Int, val bytes: Long) : Event

    /** [manifestAt] and [manifest]: the run's own observation, from its stream's manifest. */
    data class Published(val made: CopyMade, val manifestAt: Long?, val manifest: ServerFile?) : Event

    /** The producer threw: [detail] is the exception. */
    data class Unexpected(val detail: String) : Event

    /** The producer stopped without a terminal report. */
    data object Ended : Event

    // Recovery.
    /** Running without a live producer. */
    data object Stranded : Event

    /** At the owner's start; [copyValid] is false when the copy's files are incomplete. */
    data class Recover(val copyValid: Boolean) : Event
}

/** How a run ends, as it reports it. */
sealed interface RunEnd : Event {
    data object Wait : RunEnd

    data object Unauthorized : RunEnd

    data class BrokenOff(val stored: Boolean) : RunEnd

    data class ServerError(val message: String?) : RunEnd

    data class Failed(val kind: String, val error: String?, val detail: String? = null) : RunEnd

    data class StorageFull(val error: String) : RunEnd

    /** Gone from the server, seen by a request that started at [at] (wall clock); also the reading sync's hook. */
    data class Gone(val at: Long) : RunEnd
}

/** [row] after [event], at [now]; [newId] names a new transfer directory. */
fun transition(row: DownloadEntity?, event: Event, now: Long, newId: () -> String): Change {
    if (row == null) {
        if (event !is Event.Enqueue) return Change.None
        val new = event.new
        return write(DownloadEntity(new.contentId, new.seriesId, DownloadState.QUEUED, requestedBy = new.requestedBy, queuedAt = now, transferId = newId()))
    }
    val state = row.state
    val next: DownloadEntity = when (event) {
        is Event.Enqueue -> row
        is Event.Pause -> if ((state == DownloadState.QUEUED || state == DownloadState.RUNNING) && event.expect.matches(row)) row.copy(state = DownloadState.PAUSED) else row
        Event.Resume -> if (state == DownloadState.PAUSED) row.copy(state = DownloadState.QUEUED) else row
        Event.ResumeAll -> when {
            state == DownloadState.PAUSED -> row.copy(state = DownloadState.QUEUED)
            state == DownloadState.FAILED && row.errorKind == ErrorKind.STORAGE -> row.copy(state = DownloadState.QUEUED).noErrors()
            else -> row
        }
        Event.Retry -> if (state == DownloadState.FAILED || (state == DownloadState.QUEUED && row.error != null)) {
            row.copy(state = DownloadState.QUEUED, attempts = 0).noErrors()
        } else {
            row
        }
        is Event.Damaged -> if (row.copyId != null && event.copyId == row.copyId) row.copy(damagedCopy = row.copyId) else row
        Event.Again -> if (state == DownloadState.DONE && (row.stale == Stale.VERSION || row.stale == Stale.DAMAGED)) {
            row.newTransfer(newId).copy(state = DownloadState.QUEUED, queuedAt = now, attempts = 0).noErrors()
        } else {
            row
        }
        is Event.Cancel -> when {
            state == DownloadState.DONE || !event.expect.matches(row) -> row
            row.copyId == null -> return Change.Delete
            else -> row.backToCopy()
        }
        Event.Delete -> return Change.Delete
        Event.Keep -> row.copy(keep = true)
        is RunEnd.Gone -> when {
            !row.accepts(event.at) -> row
            row.copyId != null -> row.see(event.at, null).backToCopy()
            else -> row.see(event.at, null).newTransfer(newId)
                .copy(state = DownloadState.FAILED, error = DownloadError.GONE, errorKind = ErrorKind.SERVER, errorDetail = null)
        }
        is Event.Observe -> if (row.accepts(event.at)) row.see(event.at, event.server) else row
        Event.Claim -> if (state == DownloadState.QUEUED) row.copy(state = DownloadState.RUNNING) else row
        Event.Restarted -> row.newTransfer(newId)
        is Event.Manifest -> if (row.pagesDone == 0) {
            val m = event.manifest
            row.copy(version = m.version, pageCount = m.pageCount, fileSize = m.fileSize, fileMtime = m.fileMtime)
        } else {
            row
        }
        is Event.Page -> if (event.index == row.pagesDone && (row.pageCount == null || event.index < row.pageCount)) {
            row.copy(pagesDone = row.pagesDone + 1, bytesDone = row.bytesDone + event.bytes, attempts = 0)
        } else {
            row
        }
        is Event.Published -> if (row.pageCount != null && row.pagesDone == row.pageCount) {
            val copy = row.copy(
                copyId = row.transferId, copyVersion = row.version, copyPageCount = row.pageCount, copyBytes = event.made.bytes,
                copyFileSize = row.fileSize, copyFileMtime = row.fileMtime, copyCover = event.made.cover, copySeriesCover = event.made.seriesCover,
                completedAt = now, damagedCopy = null,
            )
            // The run's manifest is an observation too, unless a newer one is stored.
            val at = event.manifestAt
            (if (at != null && row.accepts(at)) copy.see(at, event.manifest) else copy).backToCopy()
        } else {
            row
        }
        RunEnd.Wait -> row.copy(state = DownloadState.QUEUED, error = DownloadError.UNREACHABLE, errorKind = ErrorKind.NETWORK, errorDetail = null)
        RunEnd.Unauthorized -> row.copy(state = DownloadState.QUEUED)
        is RunEnd.BrokenOff -> {
            val attempts = if (event.stored) 0 else row.attempts + 1
            if (attempts >= MAX_ATTEMPTS) {
                row.copy(state = DownloadState.FAILED, attempts = attempts, error = DownloadError.BROKEN_OFF, errorKind = ErrorKind.NETWORK, errorDetail = null)
            } else {
                row.copy(state = DownloadState.QUEUED, attempts = attempts, error = DownloadError.UNREACHABLE, errorKind = ErrorKind.NETWORK, errorDetail = null)
            }
        }
        is RunEnd.ServerError -> {
            val attempts = row.attempts + 1
            if (attempts >= MAX_ATTEMPTS) {
                row.copy(state = DownloadState.FAILED, attempts = attempts, error = event.message, errorKind = ErrorKind.SERVER, errorDetail = null)
            } else {
                // To the end of the queue, so one broken item doesn't starve the others.
                row.copy(state = DownloadState.QUEUED, attempts = attempts, queuedAt = now, error = event.message, errorKind = ErrorKind.SERVER, errorDetail = null)
            }
        }
        is RunEnd.Failed -> row.copy(state = DownloadState.FAILED, error = event.error, errorKind = event.kind, errorDetail = event.detail)
        is RunEnd.StorageFull -> row.copy(state = DownloadState.FAILED, error = event.error, errorKind = ErrorKind.STORAGE, errorDetail = null)
        is Event.Unexpected -> row.copy(state = DownloadState.FAILED, error = DownloadError.UNEXPECTED, errorKind = ErrorKind.PROTOCOL, errorDetail = event.detail)
        Event.Ended -> row.copy(state = DownloadState.QUEUED)
        Event.Stranded -> if (state == DownloadState.RUNNING) row.copy(state = DownloadState.QUEUED) else row
        is Event.Recover -> {
            val queued = if (state == DownloadState.RUNNING) row.copy(state = DownloadState.QUEUED) else row
            when {
                row.copyId == null || event.copyValid -> queued
                state == DownloadState.DONE -> queued.noCopy().newTransfer(newId)
                    .copy(state = DownloadState.FAILED, error = DownloadError.MISSING_FILES, errorKind = ErrorKind.STORAGE, errorDetail = null)
                else -> queued.noCopy()
            }
        }
    }
    return if (next == row) Change.None else write(next)
}

/**
 * Every write carries the derived stale mark, and keeps the row's invariants (I1 to I3). In order: gone,
 * damaged (while [DownloadEntity.damagedCopy] names the copy), then the version.
 */
private fun write(row: DownloadEntity): Change.Write {
    val seen = row.seenAt != null
    val stale = when {
        row.copyId == null -> null
        seen && row.seenGone -> Stale.GONE
        row.damagedCopy == row.copyId -> Stale.DAMAGED
        !seen -> null
        else -> staleOf(row.copyFileMtime, row.copyFileSize, ServerFile(true, row.seenMtime, row.seenSize))
    }
    val derived = row.copy(stale = stale)
    check((derived.state == DownloadState.DONE) == (derived.transferId == null)) { "I1: ${derived.contentId}" }
    check(derived.copyId != null || derived.transferId != null) { "I2: ${derived.contentId}" }
    return Change.Write(derived)
}

private fun String?.matches(row: DownloadEntity) = this == null || this == row.transferId

/** An observation from a request started at [at] is no older than the stored one. */
private fun DownloadEntity.accepts(at: Long) = seenAt == null || at >= seenAt

private fun DownloadEntity.see(at: Long, server: ServerFile?): DownloadEntity {
    val file = server?.takeIf { it.valid }
    return copy(seenAt = at, seenGone = file == null, seenMtime = file?.fileMtime, seenSize = file?.fileSize)
}

private fun DownloadEntity.noErrors() = copy(error = null, errorKind = null, errorDetail = null)

private fun DownloadEntity.newTransfer(newId: () -> String) = noTransfer().copy(transferId = newId())

private fun DownloadEntity.noTransfer() = copy(transferId = null, version = null, pageCount = null, pagesDone = 0, bytesDone = 0, fileSize = null, fileMtime = null)

/** Done with its copy: the transfer, and whatever it failed with, dropped. */
private fun DownloadEntity.backToCopy() = noTransfer().noErrors().copy(state = DownloadState.DONE, attempts = 0)

private fun DownloadEntity.noCopy() = copy(
    copyId = null, copyVersion = null, copyPageCount = null, copyBytes = 0, copyFileSize = null, copyFileMtime = null,
    copyCover = false, copySeriesCover = false, completedAt = null, damagedCopy = null,
)
