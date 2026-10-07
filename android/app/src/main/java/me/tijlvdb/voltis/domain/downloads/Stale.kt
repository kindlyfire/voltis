package me.tijlvdb.voltis.domain.downloads

import java.time.OffsetDateTime
import java.time.format.DateTimeParseException

/** Why a download no longer matches the server; it stays readable (P2 §8). */
object Stale {
    const val VERSION = "version"
    const val GONE = "gone"

    /** The copy's own files are short or won't decode (`damaged_copy` names the copy); online it is read from the server. */
    const val DAMAGED = "damaged"
}

/** "Download again" replaces a copy that is outdated or damaged, not one that is gone. */
fun String?.offersAgain() = this == Stale.VERSION || this == Stale.DAMAGED

/** A local page that couldn't be read back: the copy is damaged, and "Download again" restores it. */
class PageDamaged(cause: Throwable? = null) : Exception("Page is damaged", cause)

/**
 * A copy's files as published: pages 0 until [pageCount] each there and not empty, and the
 * directory's files adding up to [copyBytes], as Transfer.complete measured them (0: unknown, not compared).
 * [pageLength] is null for a missing file.
 */
fun filesIntact(pageCount: Int, copyBytes: Long, pageLength: (Int) -> Long?, dirBytes: Long): Boolean =
    (0 until pageCount).all { (pageLength(it) ?: 0L) > 0L } && (copyBytes <= 0L || dirBytes == copyBytes)

/** What the server lists for a downloaded item: its file's mtime and size. */
data class ServerFile(val valid: Boolean, val fileMtime: String?, val fileSize: Long?)

/**
 * A done download's stale mark (P2 §8, Stale detection) from the rows alone: [server] null (missing
 * from its series' list, or a 404) or not valid is [Stale.GONE]; another mtime (as instants: the
 * manifest's is UTC, the API's has the server's offset) or size is [Stale.VERSION], since the
 * offline version hashes exactly these; else null. An unknown value on either side is no difference.
 */
fun staleOf(downloadMtime: String?, downloadSize: Long?, server: ServerFile?): String? = when {
    server == null || !server.valid -> Stale.GONE
    differs(downloadMtime, server.fileMtime) { instant(it) } || differs(downloadSize, server.fileSize) { it } -> Stale.VERSION
    else -> null
}

private fun <T, K> differs(a: T?, b: T?, key: (T) -> K): Boolean = a != null && b != null && key(a) != key(b)

/** An RFC 3339 time as an instant; an unparsable one compares as the text it is. */
private fun instant(time: String): Any = try {
    OffsetDateTime.parse(time).toInstant()
} catch (_: DateTimeParseException) {
    time
}
