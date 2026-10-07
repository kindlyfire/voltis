package me.tijlvdb.voltis.data.reader

import java.io.File
import javax.inject.Inject
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.content.ContentRepository
import me.tijlvdb.voltis.data.content.pageUrl
import me.tijlvdb.voltis.domain.comic.OpenedComic
import me.tijlvdb.voltis.domain.comic.PageSource
import me.tijlvdb.voltis.domain.comic.ReaderData
import me.tijlvdb.voltis.domain.comic.pageDimensions

/** Reader data whose opens can be made for one account. */
interface AccountReaderData : ReaderData {
    /** As `open`, refused with `AccountChangedException` once [account] isn't the signed-in one. Nothing to keep: there is no owner. */
    suspend fun open(contentId: String, account: ForAccount?): OpenedComic
}

/** The reader's data straight from the server. */
class NetworkReaderData @Inject constructor(
    private val content: ContentRepository,
    private val preloader: PagePreloader,
) : AccountReaderData {
    override suspend fun open(contentId: String, owner: CoroutineScope): OpenedComic = open(contentId, null)

    override suspend fun open(contentId: String, account: ForAccount?): OpenedComic {
        val comic = content.comicWithPageSizes(contentId, account)
        return OpenedComic(
            comic,
            object : PageSource {
                override val pages = comic.fileData.pageDimensions()

                override fun page(index: Int) = pageUrl(comic, index).toString()

                override suspend fun preload(index: Int) = preloader.toDisk(pageUrl(comic, index))

                override suspend fun <T> read(index: Int, block: (File) -> T): T = preloader.read(pageUrl(comic, index), block)
            },
        )
    }

    override suspend fun content(id: String): Content = content.get(id)

    override suspend fun volumes(seriesId: String): List<Content> = content.volumes(seriesId)

    override suspend fun earlierUnread(seriesId: String): String? = content.continueTarget(seriesId).earlierUnreadId

    override suspend fun readable(contentId: String) = true

    override fun unreadable(ids: Set<String>, unreachable: Boolean): Flow<Set<String>> = flowOf(emptySet())
}
