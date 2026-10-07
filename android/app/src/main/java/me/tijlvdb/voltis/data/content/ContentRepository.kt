package me.tijlvdb.voltis.data.content

import javax.inject.Inject
import javax.inject.Singleton
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentPage
import me.tijlvdb.voltis.data.api.ContinueEntry
import me.tijlvdb.voltis.data.api.ContinueTarget
import me.tijlvdb.voltis.data.api.FileData
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.downloads.isDetail
import me.tijlvdb.voltis.data.reading.ReadingIngest

/**
 * A thin wrapper over the content endpoints: nothing is cached, but the reading state of every row
 * an answer carries is imported (`reading_snapshot`) before the caller sees it. A request that is
 * imported is made for the account of the store it is imported into, so that only that account's
 * session can answer it.
 */
@Singleton
class ContentRepository internal constructor(
    private val api: VoltisApi,
    private val ingest: ReadingIngest = ReadingIngest.None,
    /** The live session's account: the open store counts only while it is that account's. Null: not checked. */
    private val session: (() -> String?)? = null,
) {
    @Inject
    constructor(api: VoltisApi, ingest: ReadingIngest, session: SessionStore) : this(api, ingest, { session.state.value.account })

    /**
     * The generation of the open account: its store, while that is the live session's. Work captures it when it starts
     * and hands it to every memo and hint call it makes, so work of an account that has gone never touches the next one.
     */
    fun owner(): AccountStore? = ingest.open()?.takeIf { store -> session?.let { it() == store.account } != false }

    private val memo = ContentMemo(live = ::owner)
    private val shown = ShownHints(live = ::owner)

    /** What an earlier list or fetch of [owner] returned for [id], as the server said it: a page's first frame until its own fetch lands. */
    fun known(owner: Any?, id: String): Content? = memo.get(owner, id)

    /** A whole list of a series' contents asked before, as the server said it. */
    fun knownList(owner: Any?, params: ContentListParams): List<Content>? = memo.list(owner, params)

    /** A row read from the store, as it was fetched: it counts as detail when it has what only a single fetch has. */
    fun adopt(owner: Any?, content: Content) = memo.put(owner, listOf(content), detail = content.isDetail)

    /** [id] is gone or off limits: nothing of it is kept. */
    fun forget(owner: Any?, id: String) {
        memo.forget(owner, id)
        shown.forget(owner, id)
    }

    /** What the last page showed of [id]'s reading, for a first frame only. */
    fun shownHint(owner: Any?, id: String): UserData? = shown.get(owner, id)

    fun hasShownHint(owner: Any?, id: String) = shown.contains(owner, id)

    /** Whether the series last had rows in its downloads line: its place on a first frame. Null: never seen. */
    fun downloadsLine(owner: Any?, seriesId: String): Boolean? = shown.flag(owner, seriesId)

    fun rememberDownloadsLine(owner: Any?, seriesId: String, present: Boolean) = shown.putFlag(owner, seriesId, present)

    fun rememberShown(owner: Any?, items: Map<String, UserData?>) = shown.put(owner, items)

    /** [call] gets the account to tag its request with: the explicit [account], or the store's. */
    private suspend fun <T> ingesting(
        rows: (T) -> List<Content?>,
        account: ForAccount? = null,
        memoize: Boolean = true,
        detail: Boolean = false,
        call: suspend (ForAccount?) -> T,
    ): T {
        // The store the request is made for: its rows are imported and kept only while it is still the open one.
        val asked = ingest.open(account?.account)
        val kept = asked?.takeIf { it === owner() }
        return call(asked?.let { ForAccount(it.account) } ?: account).also {
            ingest.accept(asked, rows(it))
            if (memoize) memo.put(kept, rows(it), detail)
        }
    }

    suspend fun get(id: String): Content = ingesting({ listOf(it) }, detail = true) { api.contentById(id, it) }

    /** A comic with the size of every page; for [account], when given. */
    suspend fun comicWithPageSizes(id: String, account: ForAccount? = null): Content =
        ingesting({ listOf(it.copy(fileData = FileData())) }, account, detail = true) { api.comicWithPageSizes(id, it) }

    suspend fun list(params: ContentListParams): ContentPage {
        val asked = owner()
        return ingesting({ it.data }) { api.content(params.toQuery(), it) }.also { memo.putList(asked, params, it.data) }
    }

    /** A series' volumes in its order. Without a limit the server returns every one. */
    suspend fun volumes(seriesId: String): List<Content> =
        list(ContentListParams.volumes(seriesId)).data

    /** The first ID of a list, or null when it is empty. */
    suspend fun firstId(params: ContentListParams): String? =
        api.contentIds(params.copy(offset = 0, limit = 1).toQuery()).ids.firstOrNull()

    /** [timeout] (seconds) bounds the connect and the read, for a caller that falls back without the server. */
    suspend fun continueReading(limit: Int, timeout: Int? = null): List<ContinueEntry> =
        ingesting({ rows -> rows.flatMap { listOf(it.item, it.series) } }) { api.continueReading(limit, timeout, timeout, it) }

    /** What reading [id] continues with. */
    suspend fun continueTarget(id: String): ContinueTarget = ingesting({ listOf(it.target) }) { api.continueTarget(id, it) }
}
