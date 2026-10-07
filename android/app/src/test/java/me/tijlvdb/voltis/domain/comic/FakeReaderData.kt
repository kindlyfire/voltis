package me.tijlvdb.voltis.domain.comic

import java.io.File
import java.io.IOException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.isActive
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.map
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType

fun volume(id: String, parentId: String? = null, type: String = ContentType.COMIC) = Content(id, id, type, parentId = parentId)

/** A server of made-up comics: each content's series, and each series' volumes. */
class FakeReaderData : ReaderData {
    val comics = mutableMapOf<String, Content>()
    val lists = mutableMapOf<String, List<String>>()
    var pageCount = 3

    /** Tall pages make the mode's Auto Longstrip. */
    var pageSize = PageDimensions(800, 1200)

    /** Lists fail this many times. */
    var failLists = 0

    /** When set, `volumes` waits for it. */
    var listGate: CompletableDeferred<Unit>? = null

    /** When set, `open` waits for it. */
    var openGate: CompletableDeferred<Unit>? = null

    /** IDs that can't be opened now (offline and not downloaded); it can change while a reader is open. */
    val offline = MutableStateFlow(emptySet<String>())

    /** Emits to what was opened last: its copy was replaced. */
    val replaced = MutableSharedFlow<Unit>(extraBufferCapacity = 1)
    val calls = mutableListOf<String>()
    val preloaded = mutableListOf<Int>()

    /** Opens fail this many times. */
    var failOpens = 0

    /** The owner of each open, in order: its files are kept while it is active. */
    val owners = mutableListOf<CoroutineScope>()
    val kept get() = owners.count { it.isActive }

    override suspend fun open(contentId: String, owner: CoroutineScope): OpenedComic {
        owners += owner
        openGate?.await()
        if (failOpens > 0) {
            failOpens--
            throw IOException("down")
        }
        return OpenedComic(
            comics.getValue(contentId),
            object : PageSource {
                override val pages = List(pageCount) { pageSize }

                override fun page(index: Int) = "page:$index"

                override suspend fun preload(index: Int) {
                    preloaded += index
                }

                override suspend fun <T> read(index: Int, block: (File) -> T): T = throw IOException("no files")
            },
            replaced,
        )
    }

    override suspend fun content(id: String): Content {
        calls += "content:$id"
        return comics[id] ?: Content(id, id, ContentType.COMIC_SERIES)
    }

    override suspend fun volumes(seriesId: String): List<Content> {
        calls += "volumes:$seriesId"
        listGate?.await()
        if (failLists > 0) {
            failLists--
            throw IOException("down")
        }
        return lists[seriesId].orEmpty().map { volume(it, seriesId) }
    }

    /** What `earlierUnread` answers, by series. */
    val earlier = mutableMapOf<String, String>()

    override suspend fun earlierUnread(seriesId: String): String? = earlier[seriesId]

    override suspend fun readable(contentId: String) = contentId !in offline.value

    override fun unreadable(ids: Set<String>, unreachable: Boolean): Flow<Set<String>> = offline.map { ids intersect it }
}
