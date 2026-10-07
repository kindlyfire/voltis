package me.tijlvdb.voltis.data.downloads

import java.io.IOException
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import me.tijlvdb.voltis.data.api.AppJson
import okio.Buffer
import okio.BufferedSource
import okio.Source
import okio.buffer

// The body of `GET api/files/offline/<id>?from=N` (backend/routes/files.go, `offline`): a manifest
// frame, then page frames in order, then an end or error frame. Integers are big-endian u32.

@Serializable
data class OfflineManifest(
    val format: Int,
    @SerialName("content_id") val contentId: String,
    val version: String,
    @SerialName("file_size") val fileSize: Long? = null,
    @SerialName("file_mtime") val fileMtime: String? = null,
    @SerialName("page_count") val pageCount: Int,
    val from: Int,
    /** Every page, whatever [from] is. */
    val pages: List<OfflinePage>,
)

/** [width] and [height] are pixels, null when unknown. */
@Serializable
data class OfflinePage(
    val name: String,
    @SerialName("media_type") val mediaType: String? = null,
    val width: Int? = null,
    val height: Int? = null,
)

/** Not an IOException: a failure to write a page is the sink's, and stays one. */
sealed class OfflineError(message: String?, cause: Throwable? = null) : Exception(message, cause) {
    /** The server's error frame, after the pages before it. */
    class Page(message: String) : OfflineError(message)

    class Protocol(message: String) : OfflineError(message)

    /** The body ended, or failed to read, before an end or error frame. */
    class Truncated(cause: Throwable? = null) : OfflineError("The download was cut off", cause)
}

interface OfflineSink {
    /** Return false to stop reading (a changed version). */
    suspend fun manifest(manifest: OfflineManifest): Boolean

    /** Consume exactly [length] bytes from [data]; a read from it that fails throws [OfflineError.Truncated]. */
    suspend fun page(index: Int, length: Long, data: BufferedSource)
}

private const val MAX_MANIFEST = 4L shl 20
private const val MAX_PAGE = 256L shl 20
private const val MAX_MESSAGE = 64L shl 10
private const val END = 0xFFFFFFFFL
private const val ERROR = 0xFFFFFFFEL

/** Reads a whole stream asked for with `from` = [from] into [sink]; throws [OfflineError] unless it ended cleanly. */
suspend fun readOfflineStream(source: BufferedSource, from: Int, sink: OfflineSink) {
    val manifestLength = source.u32()
    if (manifestLength > MAX_MANIFEST) throw OfflineError.Protocol("Manifest too large")
    val json = source.bytes(manifestLength).utf8()
    val manifest = try {
        AppJson.decodeFromString(OfflineManifest.serializer(), json)
    } catch (e: IllegalArgumentException) {
        throw OfflineError.Protocol("Unreadable manifest")
    }
    when {
        manifest.format != 1 -> throw OfflineError.Protocol("Unknown format ${manifest.format}")
        manifest.pageCount != manifest.pages.size -> throw OfflineError.Protocol("Page count mismatch")
        manifest.from != from || from !in 0..manifest.pageCount -> throw OfflineError.Protocol("Unexpected start ${manifest.from}")
    }
    if (!sink.manifest(manifest)) return
    var next = from
    while (true) {
        val index = source.u32()
        val length = source.u32()
        when {
            index == END -> {
                if (length != 0L || next != manifest.pageCount) throw OfflineError.Protocol("Early end at page $next")
                if (!source.reading { exhausted() }) throw OfflineError.Protocol("Data after the end")
                return
            }
            index == ERROR -> {
                if (length > MAX_MESSAGE) throw OfflineError.Protocol("Error message too large")
                throw OfflineError.Page(source.bytes(length).utf8())
            }
            index != next.toLong() || next >= manifest.pageCount -> throw OfflineError.Protocol("Page $index out of order")
            length > MAX_PAGE -> throw OfflineError.Protocol("Page $index too large")
        }
        val data = LimitedSource(source, length).buffer()
        sink.page(next, length, data)
        // A sink that left bytes behind would desynchronise the frames.
        check(data.exhausted()) { "Page $next not consumed" }
        next++
    }
}

private inline fun <T> BufferedSource.reading(block: BufferedSource.() -> T): T = try {
    block()
} catch (e: IOException) {
    throw OfflineError.Truncated(e)
}

private fun BufferedSource.u32(): Long = reading { readInt().toLong() and 0xFFFFFFFFL }

private fun BufferedSource.bytes(length: Long) = reading { readByteString(length) }

/** The next [remaining] bytes of [upstream]; ending early or failing to read is [OfflineError.Truncated]. */
private class LimitedSource(private val upstream: BufferedSource, private var remaining: Long) : Source {
    override fun read(sink: Buffer, byteCount: Long): Long {
        if (remaining == 0L) return -1
        val n = upstream.reading { read(sink, minOf(byteCount, remaining)) }
        if (n == -1L) throw OfflineError.Truncated()
        remaining -= n
        return n
    }

    override fun timeout() = upstream.timeout()

    override fun close() {}
}
