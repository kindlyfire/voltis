package me.tijlvdb.voltis.domain.downloads

/** A download row's `state` (P2 §3). */
object DownloadState {
    const val QUEUED = "queued"
    const val RUNNING = "running"
    const val PAUSED = "paused"
    const val FAILED = "failed"
    const val DONE = "done"
}

/** A failed or waiting download's `error_kind`. */
object ErrorKind {
    const val NETWORK = "network"
    const val SERVER = "server"
    const val STORAGE = "storage"
    const val PAGE = "page"
    const val PROTOCOL = "protocol"
}

/** A card's download mark (P2 §11); a stale download is [UPDATED] or [GONE] by its stale kind. */
enum class DownloadBadge { QUEUED, DOWNLOADING, DOWNLOADED, FAILED, UPDATED, DAMAGED, GONE }

/**
 * An item's mark from its row. With a copy it is downloaded (or why it is stale), unless a
 * replacement is running; without one, its state. A paused row counts as queued: it is still on its way.
 */
fun downloadBadge(state: String, hasCopy: Boolean, stale: String?): DownloadBadge = when {
    state == DownloadState.RUNNING -> DownloadBadge.DOWNLOADING
    hasCopy -> when (stale) {
        Stale.VERSION -> DownloadBadge.UPDATED
        Stale.DAMAGED -> DownloadBadge.DAMAGED
        Stale.GONE -> DownloadBadge.GONE
        else -> DownloadBadge.DOWNLOADED
    }
    state == DownloadState.FAILED -> DownloadBadge.FAILED
    else -> DownloadBadge.QUEUED
}

object RequestedBy {
    const val USER = "user"
    const val AUTO = "auto"
}

/** Tries of one item that end without a stored page (or with a Voltis 500) before it fails. */
const val MAX_ATTEMPTS = 5

/** Errors the app sets, stored as these codes and named in the UI's strings; any other `error` is the server's message. */
object DownloadError {
    const val UNREACHABLE = "unreachable"
    const val BROKEN_OFF = "broken_off"
    const val NO_SPACE = "no_space"
    const val WRITE_FAILED = "write_failed"
    const val GONE = "gone"
    const val FILE_CHANGED = "file_changed"
    /** A copy's files went missing (found at the start); Retry is "Download again". */
    const val MISSING_FILES = "missing_files"

    /** A stream the app couldn't read; the parser's message is in `error_detail`. */
    const val PROTOCOL = "protocol"

    /** A bug or a database failure while downloading; the message is in `error_detail`. */
    const val UNEXPECTED = "unexpected"
}
