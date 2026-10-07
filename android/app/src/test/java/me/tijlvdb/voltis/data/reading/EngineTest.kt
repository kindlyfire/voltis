package me.tijlvdb.voltis.data.reading

import java.io.IOException
import kotlin.coroutines.CoroutineContext
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.completeWith
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.longOrNull
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.content.CatalogSignal
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.reading.Change
import me.tijlvdb.voltis.domain.reading.FakeReadingServer
import me.tijlvdb.voltis.domain.reading.InMemoryReadingStore
import me.tijlvdb.voltis.domain.reading.PositionLabel
import me.tijlvdb.voltis.domain.reading.PromptChoice
import me.tijlvdb.voltis.domain.reading.ReaderAdapter
import me.tijlvdb.voltis.domain.reading.ReaderNotice
import me.tijlvdb.voltis.domain.reading.ReadingSession
import me.tijlvdb.voltis.domain.reading.ReadingStore
import me.tijlvdb.voltis.domain.reading.ReviewSession
import me.tijlvdb.voltis.domain.reading.Stored
import me.tijlvdb.voltis.domain.reading.SyncAction
import me.tijlvdb.voltis.domain.reading.SyncNotice
import me.tijlvdb.voltis.domain.reading.SyncPrompt
import me.tijlvdb.voltis.domain.reading.Undoable
import me.tijlvdb.voltis.domain.storage.StorageFullException
import org.junit.After
import org.junit.Assert.assertTrue

/**
 * The port of readingSync.test.ts' setup: [FakeReadingServer] behind the engine, an in-memory store,
 * virtual time. `settle()` is the web's `advanceTimersByTimeAsync(0)`.
 */
abstract class EngineTest {
    protected val server = FakeReadingServer(LAST)
    protected val store = InMemoryReadingStore()

    /** The store the engine uses: [store], with faults on demand. */
    protected val faults = FaultyStore(store)
    protected var writer = FakeWriter()

    /** Its probe passes while [server] answers. */
    protected val connectivity = FakeConnectivity { !server.unreachable }
    protected val scheduler = FakeScheduler()
    /** What the engines reported as unexpected: a test that expects some clears it. */
    protected val errors = mutableListOf<Throwable>()

    /** Errors that would have ended the process. */
    protected val crashes = mutableListOf<Throwable>()

    /** Items the engine reported gone, for their downloads. */
    protected val gone = mutableListOf<String>()

    @After
    fun oneAtATime() {
        // Whatever happened, no two reading requests were ever out at once.
        assertTrue("${server.maxInFlight} requests out at once", server.maxInFlight <= 1)
        errors.firstOrNull()?.let { throw it }
    }

    /**
     * Runs [run] once per case. The fixture lives on the test instance, so the first case uses this one
     * and each later case gets a fresh instance, checked as `oneAtATime` is after a test.
     */
    protected fun <T : EngineTest, C> T.eachCase(cases: List<C>, run: T.(C) -> Unit) {
        cases.forEachIndexed { i, case ->
            val test = if (i == 0) this else javaClass.getDeclaredConstructor().newInstance()
            try {
                test.run(case)
                if (i > 0) test.oneAtATime()
            } catch (e: Throwable) {
                throw AssertionError("case $case: ${e.message}", e)
            }
        }
    }

    /** [before] runs ahead of the engine's start, to put rows in [store]. */
    protected fun engineTest(before: suspend () -> Unit = {}, body: suspend Harness.() -> Unit) = runTest {
        before()
        Harness(this).body()
    }

    class FakeWriter(override val writerId: String = "tenginetest000000") : ReadingWriter {
        var seq = 0L
        var broken = false

        /** The identity file can't be written: a full disk. */
        var full = false
        var reservations = 0

        override suspend fun ensureReserved() {
            reservations++
            if (full) throw StorageFullException()
            if (broken) throw IOException("Couldn't reserve a seq")
        }

        override fun nextSeq() = ++seq
    }

    class StoreFault : IOException("Injected store failure")

    class FakeConnectivity(var reachable: () -> Boolean) : Connectivity {
        override val online = MutableStateFlow(true)
        var probes = 0

        /** Another component's probe fails right after an answer: `online` is never seen true. */
        var failsAfterAnswer = false

        override suspend fun probe(): Boolean {
            probes++
            return reachable().also { online.value = it }
        }

        override fun answered() {
            online.value = true
            if (failsAfterAnswer) online.value = false
        }

        override fun unreachable() = Unit
    }

    class FakeScheduler : SyncScheduler {
        var scheduled = 0

        override fun schedule() {
            scheduled++
        }
    }

    /** [inner], with commits that fail or wait on demand, and a load that fails. */
    class FaultyStore(private val inner: ReadingStore) : ReadingStore by inner {
        /** Commits it matches throw [StoreFault]. */
        var fail: ((Change) -> Boolean)? = null

        /** Commits it matches wait for [release]. */
        var hold: ((Change) -> Boolean)? = null
        private var held = CompletableDeferred<Unit>()
        var failLoad = false

        /** Lane lookups it matches throw [StoreFault]. */
        var failLane: ((String) -> Boolean)? = null

        fun release() {
            hold = null
            held.complete(Unit).also { held = CompletableDeferred() }
        }

        override suspend fun load(): Stored = if (failLoad) throw StoreFault() else inner.load()

        override suspend fun lane(contentId: String) = if (failLane?.invoke(contentId) == true) throw StoreFault() else inner.lane(contentId)

        override suspend fun commit(change: Change): List<Long> {
            if (hold?.invoke(change) == true) held.await()
            if (fail?.invoke(change) == true) throw StoreFault()
            return inner.commit(change)
        }
    }

    data class Opened(val sync: ReadingSession, val placements: List<JsonObject>, val opened: JsonObject)

    class Reader(val sync: ReadingSession, val placements: MutableList<JsonObject>, val loading: CompletableDeferred<JsonObject>)

    @OptIn(ExperimentalCoroutinesApi::class)
    inner class Harness(private val test: TestScope) {
        val testScope get() = test

        val changes = mutableListOf<CatalogChange>()
        val notices = mutableListOf<ReaderNotice>()
        private val sessions = mutableListOf<ReadingSession>()
        val catalog = CatalogEvents()

        /** Every catalog signal, in emission order. */
        val signals = mutableListOf<CatalogSignal>()
        private val unconfined = UnconfinedTestDispatcher(test.testScheduler)
        var engine = newEngine()

        init {
            test.backgroundScope.launch(unconfined) { catalog.signals.collect { signal -> signals += signal; if (signal is CatalogSignal.Changed) changes += signal.change } }
            engine.start()
        }

        fun newEngine(
            parent: CoroutineScope = test.backgroundScope,
            main: CoroutineContext = StandardTestDispatcher(test.testScheduler),
            onGone: (String, Long) -> Unit = { id, _ -> gone += id },
        ) = ReadingEngine(
            faults, server, writer, catalog, connectivity, scheduler, parent,
            main = main,
            clock = { test.testScheduler.currentTime },
            onError = { if (it !is StoreFault && it !is StorageFullException) errors += it },
            onGone = onGone,
            crash = { crashes += it },
            accountDir = ACCOUNT_DIR,
        )

        /** A new engine on the same store, as after a kill: the old one is stopped first, then [between] runs. */
        suspend fun restart(between: () -> Unit = {}) {
            engine.stop()
            between()
            sessions.clear()
            engine = newEngine().also { it.start() }
            settle()
        }

        /** A reader on item [id], placing itself as the real ones do, with its load not yet awaited. */
        fun attach(id: String): Reader {
            val placements = mutableListOf<JsonObject>()
            lateinit var sync: ReadingSession
            sync = engine.attach(
                id,
                object : ReaderAdapter {
                    override fun content() = Content(id, id, ContentType.COMIC, parentId = server.parents[id])

                    override fun restore(progress: JsonObject) {
                        placements += progress
                        sync.placed(progress)
                    }

                    override fun describe(progress: JsonObject) = PositionLabel.Page((progress.int("current_page") ?: 0) + 1)

                    override fun samePosition(a: JsonObject, b: JsonObject) = samePage(a, b)

                    override fun samePlace(a: JsonObject, b: JsonObject) = samePage(a, b)
                },
            )
            sessions += sync
            test.backgroundScope.launch(unconfined) { sync.notices.collect { notices += it } }
            val loading = CompletableDeferred<JsonObject>()
            test.backgroundScope.launch { loading.completeWith(runCatching { sync.load() }) }
            return Reader(sync, placements, loading)
        }

        /** A reviewer of [id], as "Needs attention" makes one; its prompt counts for [prompt]. */
        fun review(id: String): ReviewSession {
            val session = engine.review(id)
            sessions += session
            test.backgroundScope.launch(unconfined) { session.notices.collect { notices += it } }
            settle()
            return session
        }

        /** The row of [id] as a page fetched it from [server] just now. */
        fun fetched(id: String, type: String = ContentType.COMIC, parentId: String? = server.parents[id]): Content {
            val state = server.rows[id]
            return Content(
                id, "Title $id", type, parentId = parentId,
                userData = state?.let { UserData(status = it.status, progress = it.progress, revision = it.revision, lastReadAt = it.lastReadAt, readingSeq = it.seq) },
            )
        }

        fun now() = test.testScheduler.currentTime

        suspend fun open(id: String): Opened {
            val reader = attach(id)
            settle()
            return Opened(reader.sync, reader.placements, reader.loading.await())
        }

        fun settle() = test.runCurrent()

        fun advance(ms: Long) {
            test.advanceTimeBy(ms)
            test.runCurrent()
        }

        /** Real reading, written after the debounce. */
        fun read(sync: ReadingSession, n: Int) {
            sync.moved(page(n))
            advance(1000)
        }

        /** A command whose answer isn't awaited, posted at once. */
        fun later(block: suspend () -> Unit) {
            test.backgroundScope.launch(unconfined) { runCatching { block() } }
        }

        fun writes() = server.sent.mapNotNull { it.body }

        fun ops() = writes().map { it.string("op") }

        /** The open dialog, whichever reader shows it. */
        fun prompt(): SyncPrompt? = sessions.firstNotNullOfOrNull { it.prompt.value }

        fun answer(choice: PromptChoice) {
            sessions.first { it.prompt.value != null }.answer(choice)
            settle()
        }

        /** The latest snackbar of [type]. */
        inline fun <reified T> notice() = notices.last { it.notice is T }

        /** The latest offer of an Undo for [what]. */
        fun offer(what: Undoable) = notices.last { (it.notice as? SyncNotice.UndoOffer)?.what == what }

        /** The latest failure of [what], with its Retry. */
        fun failure(what: SyncAction) = notices.last { (it.notice as? SyncNotice.Failed)?.what == what }
    }

    companion object {
        const val LAST = 9

        /** The key of the engines' [ReadingHolds]. */
        const val ACCOUNT_DIR = "engine-test"

        fun page(n: Int) = JsonObject(mapOf("current_page" to JsonPrimitive(n)))

        val END = JsonObject(mapOf("current_page" to JsonPrimitive(LAST), "at_end" to JsonPrimitive(true)))

        fun status(value: String?) = JsonObject(mapOf("op" to JsonPrimitive("set_status"), "status" to JsonPrimitive(value)))

        fun op(op: String, vararg fields: Pair<String, String>) =
            JsonObject(mapOf("op" to JsonPrimitive(op)) + fields.associate { (k, v) -> k to JsonPrimitive(v) })

        fun JsonObject.string(key: String) = (this[key] as? JsonPrimitive)?.contentOrNull

        fun JsonObject.int(key: String) = (this[key] as? JsonPrimitive)?.intOrNull

        fun JsonObject.long(key: String) = (this[key] as? JsonPrimitive)?.longOrNull

        fun JsonObject.progress() = this["progress"] as? JsonObject

        private fun samePage(a: JsonObject, b: JsonObject) = a.page() == b.page()

        private fun JsonObject.page() = if (this["at_end"] == JsonPrimitive(true)) LAST else int("current_page")
    }
}
