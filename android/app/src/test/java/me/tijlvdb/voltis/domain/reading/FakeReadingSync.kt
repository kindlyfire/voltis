package me.tijlvdb.voltis.domain.reading

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonPrimitive
import me.tijlvdb.voltis.data.api.Content

/** A [ReadingSync] that records what its readers report. A session's `load()` answers [saved], else the content's saved progress. */
class FakeReadingSync : ReadingSync {
    val sessions = mutableListOf<FakeSession>()

    /** Where `load()` opens, by item. */
    val saved = mutableMapOf<String, JsonObject>()

    /** When set, `load()` waits for it. */
    var loadGate: CompletableDeferred<Unit>? = null

    /** Loads fail this many times. */
    var failLoads = 0

    /** Every load fails with it, when set. */
    var loadFailure: Exception? = null

    /** When set, a session's `pageCount` waits for it. */
    var pageCountGate: CompletableDeferred<Unit>? = null

    /** A session's `command` fails with it, when set. */
    var commandFailure: Exception? = null

    /** A session's `pageCount` fails with it, when set. */
    var pageCountFailure: Exception? = null

    override fun attach(contentId: String, adapter: ReaderAdapter) = FakeSession(contentId, adapter).also { sessions += it }

    inner class FakeSession(val contentId: String, val adapter: ReaderAdapter) : ReadingSession {
        /** Each call as `name` or `name:<page>`, the page being 0-based as the reader's. */
        val calls = mutableListOf<String>()
        override val view = MutableStateFlow(LaneView())
        override val prompt = MutableStateFlow<SyncPrompt?>(null)
        override val notices: Flow<ReaderNotice> = emptyFlow()

        private fun record(name: String, progress: JsonObject? = null) {
            calls += if (progress == null) name else "$name:${progress["current_page"]}"
        }

        override suspend fun load(): JsonObject {
            record("load")
            loadGate?.await()
            loadFailure?.let { throw it }
            if (failLoads > 0) {
                failLoads--
                throw IllegalStateException("down")
            }
            return saved[contentId] ?: adapter.content()?.userData?.progress ?: EmptyProgress
        }

        override suspend fun pageCount(count: Int) {
            pageCountGate?.await()
            pageCountFailure?.let { throw it }
            pageCounts += contentId to count
        }

        override fun invalidateRestores() = record("invalidateRestores")

        override fun moved(progress: JsonObject) = record("moved", progress)

        override fun placed(progress: JsonObject) = record("placed", progress)

        override fun finish(progress: JsonObject) = record("finish", progress)

        override fun flush() = record("flush")

        override suspend fun command(request: JsonObject): CommandOutcome {
            calls += "command:" + request.values.joinToString(":") { it.jsonPrimitive.content }
            commandFailure?.let { throw it }
            return CommandOutcome.APPLIED
        }

        override suspend fun seriesCommand(status: String): CommandOutcome {
            record("seriesCommand:$status")
            return CommandOutcome.APPLIED
        }

        override fun resetAndReadAgain() = record("resetAndReadAgain")

        override fun completeSeries() = record("completeSeries")

        override fun trackProgress() = record("trackProgress")

        override fun retry() = record("retry")

        override fun check() = record("check")

        override fun setVisible(visible: Boolean) = record("visible:$visible")

        override fun detach() = record("detach")

        override fun answer(choice: PromptChoice) = record("answer:$choice")

        override fun dismissPrompt() = record("dismissPrompt")
    }

    override fun review(account: String, contentId: String) = error("Not used")

    override suspend fun seed(account: String, contents: List<Content>) = Unit

    val pageCounts = mutableListOf<Pair<String, Int>>()

    override suspend fun pageCount(account: String, contentId: String, count: Int) {
        pageCounts += contentId to count
    }

    /** What [shown] emits, by item. */
    val shownRows = MutableStateFlow(emptyMap<String, Shown>())

    override fun shown(ids: Set<String>, account: String): Flow<Map<String, Shown>> = shownRows.map { rows -> rows.filterKeys { it in ids } }

    override fun attention(account: String): Flow<List<AttentionItem>> = flowOf(emptyList())

    override fun attentionCount(account: String): Flow<Int> = flowOf(0)

    override suspend fun dismiss(account: String, noticeId: Long) = Unit

    override val unsent: Flow<Int> = flowOf(0)
}
