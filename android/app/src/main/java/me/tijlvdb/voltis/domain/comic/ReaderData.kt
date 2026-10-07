package me.tijlvdb.voltis.domain.comic

import java.io.File
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.intOrNull
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.FileData

/** A width or height of 0 means the size is unknown. */
data class PageDimensions(val width: Int, val height: Int)

/** A missing size (`[name]` alone) is 0, 0. */
fun FileData.pageDimensions(): List<PageDimensions> = pages.orEmpty().map { page ->
    fun at(i: Int) = (page.getOrNull(i) as? JsonPrimitive)?.intOrNull ?: 0
    PageDimensions(at(1), at(2))
}

/** A page that is intact but can't be decoded here (a format or size the device can't take): its Retry shows, the copy is not damaged. */
class PageUnsupported(cause: Throwable? = null) : Exception("Can't decode the page", cause)

interface PageSource {
    val pages: List<PageDimensions>

    /** One page as Coil loads it: a URL string, or a downloaded page's File. */
    fun page(index: Int): Any

    /** Fetches a page ahead of its display, without decoding it. Best effort. */
    suspend fun preload(index: Int)

    /** Reads a page's image file, fetching it first if needed. Throws when the page can't be had. */
    suspend fun <T> read(index: Int, block: (File) -> T): T

    /** Keeps this source's files until closed; null once they are released. Non-suspending, no I/O. */
    fun hold(): AutoCloseable? = AutoCloseable {}

    /** A page that couldn't be shown or decoded failed with [error]: the error to show. A downloaded copy marks itself damaged. */
    fun failed(index: Int, error: Throwable): Throwable = error
}

/** [PageSource.preload] as its contract says, best effort: a failure of any kind is dropped (the page's own load reports it), a cancellation is not. */
suspend fun PageSource.preloadQuietly(index: Int) {
    try {
        preload(index)
    } catch (e: CancellationException) {
        throw e
    } catch (_: Exception) {
    }
}

/**
 * [replaced] emits once when a newer copy of the files [pages] reads is complete, or they were
 * deleted: the reader opens the item again.
 */
data class OpenedComic(val content: Content, val pages: PageSource, val replaced: Flow<Unit> = emptyFlow())

/** Everything the reader reads: from the server, or from Room and the files of a downloaded item. */
interface ReaderData {
    /**
     * The item with its page sizes, page source and saved state (`content.userData`). Downloaded
     * files stay until [owner] ends, and for as long as something holds them ([PageSource.hold]).
     */
    suspend fun open(contentId: String, owner: CoroutineScope): OpenedComic

    /** One item without its page sizes: the parent series (direction, title, item labels), or the item again for the siblings retry. */
    suspend fun content(id: String): Content

    /** The series' volumes in order, for previous/next and the volume picker. */
    suspend fun volumes(seriesId: String): List<Content>

    /** The series' earliest unread volume before where its reading stands (`GET /content/:id/continue`); null offline. */
    suspend fun earlierUnread(seriesId: String): String?

    /** Whether [contentId] can be opened now: online, or downloaded. */
    suspend fun readable(contentId: String): Boolean

    /**
     * Of [ids], those that can't be opened now, following the connection and the downloads. With
     * [unreachable] the caller found the server out of reach, whatever the connection says.
     */
    fun unreadable(ids: Set<String>, unreachable: Boolean = false): Flow<Set<String>>
}
