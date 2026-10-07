package me.tijlvdb.voltis.data.lists

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.File
import java.io.IOException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.flow.transformWhile
import kotlinx.coroutines.runBlocking
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.AccountChangedException
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.api.LocalNetworkException
import me.tijlvdb.voltis.data.api.SendOnceInterceptor
import me.tijlvdb.voltis.data.api.testApi
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.domain.sync.EntryItem
import me.tijlvdb.voltis.domain.sync.EntryKey
import me.tijlvdb.voltis.ui.UiText
import mockwebserver3.Dispatcher
import mockwebserver3.MockResponse
import mockwebserver3.MockWebServer
import mockwebserver3.RecordedRequest
import mockwebserver3.SocketEffect
import okhttp3.Headers.Companion.headersOf
import okhttp3.OkHttpClient
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** Refreshes against a fake server: what is fetched, what the cache then shows, and which account it lands in. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [37], application = Application::class)
class ListsRepositoryTest {
    private val server = MockWebServer()
    private fun db() = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), VoltisDatabase::class.java).build()
    private val storeA = AccountStore("server/a", File("unused"), db())
    private val storeB = AccountStore("server/b", File("unused"), db())
    private val current = MutableStateFlow<AccountStore?>(storeA)
    private val requests = mutableListOf<String>()
    private val scope = CoroutineScope(SupervisorJob())
    private var clock = 1_000L

    /** The signed-in account, as `ServerUrlInterceptor` sees it. */
    @Volatile private var signedIn = "server/a"

    /** The local network permission is missing: a failed request is relabelled, as `NetworkModule` does. */
    @Volatile private var noLocalNetwork = false

    private val connectivity = object : Connectivity {
        override val online = MutableStateFlow(true)
        var reachable = true
        var unreachable = 0

        override suspend fun probe() = reachable.also { online.value = it }

        override fun answered() = Unit

        override fun unreachable() {
            unreachable++
        }
    }

    /** The index's `updated_at`, `entry_count` and cover version per list; a detail answers with the same. */
    private val updated = linkedMapOf("a" to "u1", "b" to "u1")
    private val counts = mutableMapOf("a" to 2, "b" to 2)
    private val covers = mutableMapOf("a" to "v", "b" to "v")

    /** Names of the lists a create made; the others are "List {id}". */
    private val names = mutableMapOf<String, String>()

    /** Lists whose detail answers an error with Voltis' body, as (status, message). */
    private val errors = mutableMapOf<String, Pair<Int, String>>()

    /** Entry numbers whose content is gone; the first by default. */
    private val goneEntries = mutableSetOf(1)

    /** Each list's entry numbers in the order served; 1 to its count when unset. A reorder sets it. */
    private val orders = mutableMapOf<String, MutableList<Int>>()

    /** Writes as "METHOD path body", and how they are answered by path suffix. */
    private val writes = mutableListOf<String>()
    private val writeAnswers = mutableMapOf<String, Answer>()

    /** Every request in order, as "METHOD path". */
    private val log = mutableListOf<String>()

    /**
     * How the fake answers a write: [code] with [body] (null: what the server would say). [store]: it is
     * applied first. [cut]: the connection closes instead of answering.
     */
    private data class Answer(val code: Int = 200, val body: String? = null, val store: Boolean = code == 200, val cut: Boolean = false)

    /** Content IDs added to each list, served after its numbered entries. IDs starting "unknown" are no content the server has. */
    private val added = mutableMapOf<String, MutableList<String>>()

    /** Entry numbers whose `uri` a rename changed. */
    private val renamed = mutableSetOf<Int>()

    /** An entry number renamed when the next reorder is applied: the reload after it is the first to show it. */
    @Volatile private var renamedByReorder: Int? = null

    /** When set, the next write whose path ends with its first holds until it is counted down, after counting down [writeArrived]. */
    @Volatile private var writeGate: Pair<String, CountDownLatch>? = null
    private val writeArrived = CountDownLatch(1)

    /** When set, the index request waits for it after counting down [arrived]. */
    @Volatile private var gate: CountDownLatch? = null
    private val arrived = CountDownLatch(1)

    private lateinit var repository: ListsRepository

    @Before
    fun start() {
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse {
                val path = request.url.encodedPath
                synchronized(requests) { log += "${request.method} $path" }
                if (request.method != "GET") {
                    val body = request.body?.utf8().orEmpty()
                    synchronized(requests) { writes += "${request.method} $path $body".trim() }
                    writeGate?.takeIf { path.endsWith(it.first) }?.let { (_, latch) ->
                        writeGate = null
                        writeArrived.countDown()
                        latch.await(10, TimeUnit.SECONDS)
                    }
                    val answer = writeAnswers.entries.firstOrNull { path.endsWith(it.key) }?.value ?: Answer()
                    var answered = answer.body ?: """{"ok": true}"""
                    if (answer.store) store(request.method, path, body).let { answered = answer.body ?: it }
                    if (answer.cut) return MockResponse.Builder().onResponseStart(SocketEffect.CloseSocket()).build()
                    return MockResponse(answer.code, headersOf("Content-Type", "application/json"), answered)
                }
                synchronized(requests) { requests += path }
                val id = path.removePrefix("/api/custom-lists").removePrefix("/")
                val body = when {
                    id.isEmpty() -> {
                        gate?.let {
                            arrived.countDown()
                            it.await(10, TimeUnit.SECONDS)
                        }
                        updated.entries.joinToString(",", "[", "]") { (list, at) ->
                            // The first cover has no version.
                            """{"id": "$list", "updated_at": "$at", "name": "${names[list] ?: "List $list"}", "visibility": "private", "entry_count": ${counts[list]},
                                "covers": [{"id": "c-none", "cover_version": null}, {"id": "c-$list", "cover_version": "${covers[list]}"}]}"""
                        }
                    }
                    id in errors -> errors.getValue(id).let { (code, message) ->
                        return MockResponse(code, headersOf("Content-Type", "application/json"), """{"error": "$message"}""")
                    }
                    // The first entry's content is gone.
                    else -> {
                        val numbered = (orders[id] ?: (1..counts.getValue(id) - added[id].orEmpty().size).toList()).map { n ->
                            val content = if (n in goneEntries) "null" else """{"id": "c-$id-$n", "title": "Kept $n", "type": "comic_series"}"""
                            val uri = if (n in renamed) "u$n-renamed" else "u$n"
                            """{"id": "e$n", "library_id": "l", "uri": "$uri", "content": $content, "notes": null}"""
                        }
                        val extra = added[id].orEmpty().map { """{"id": "x-$it", "library_id": "l", "uri": "u-$it", "content": {"id": "$it", "title": "Made up", "type": "comic_series"}, "notes": null}""" }
                        (numbered + extra).joinToString(",", """{"id": "$id", "updated_at": "${updated[id]}", "name": "${names[id] ?: "List $id"}",
                            "visibility": "private", "entry_count": ${counts[id]}, "entries": [""", "]}")
                    }
                }
                return MockResponse(200, headersOf("Content-Type", "application/json"), body)
            }
        }
        server.start()
        // What ServerUrlInterceptor does with the tag: refuse a request made for another account before it is sent.
        val client = OkHttpClient.Builder().addInterceptor(SendOnceInterceptor()).addInterceptor { chain ->
            chain.request().tag(ForAccount::class.java)?.let { if (it.account != signedIn) throw AccountChangedException() }
            try {
                chain.proceed(chain.request())
            } catch (e: IOException) {
                throw if (noLocalNetwork) LocalNetworkException() else e
            }
        }.build()
        repository = ListsRepository(
            testApi(server.url("/"), client), current, MutableStateFlow(null), connectivity, scope, now = { clock }, elapsed = { clock },
        )
    }

    /** Applies a write to the fake's lists; answers what the server would. */
    private fun store(method: String, path: String, body: String): String {
        val ids = { key: String -> Regex(""""$key":\[(.*?)]""").find(body)!!.groupValues[1].split(",").map { it.trim('"') } }
        when {
            // An applied reorder is what the list serves afterwards, with a new updated_at.
            path.endsWith("/entries/reorder") -> {
                val list = path.removePrefix("/api/custom-lists/").substringBefore("/")
                orders[list] = Regex("e(\\d+)").findAll(body).map { it.groupValues[1].toInt() }.toMutableList()
                renamedByReorder?.let { renamed += it }
                renamedByReorder = null
                updated[list] = updated.getValue(list) + "+"
            }
            method == "POST" && path == "/api/custom-lists" -> {
                val id = "n${updated.size}"
                val name = Regex(""""name":"(.*?)"""").find(body)!!.groupValues[1]
                updated[id] = "u1"
                names[id] = name
                counts[id] = 0
                covers[id] = "v"
                return """{"id": "$id", "updated_at": "u1", "name": "$name", "visibility": "private", "entry_count": 0, "covers": []}"""
            }
            // An added entry's removal.
            method == "DELETE" && "/entries/x-" in path -> {
                val list = path.removePrefix("/api/custom-lists/").substringBefore("/")
                if (added[list]?.remove(path.substringAfter("/entries/x-")) == true) {
                    counts[list] = counts.getValue(list) - 1
                    updated[list] = updated.getValue(list) + "+"
                }
            }
            path == "/api/custom-lists/entries" -> {
                var count = 0
                for (list in ids("list_ids")) for (content in ids("ids")) {
                    if (content.startsWith("unknown") || content in added[list].orEmpty()) continue
                    added.getOrPut(list) { mutableListOf() } += content
                    counts[list] = counts.getValue(list) + 1
                    updated[list] = updated.getValue(list) + "+"
                    count++
                }
                return """{"count": $count}"""
            }
        }
        return """{"ok": true}"""
    }

    @After
    fun stop() {
        scope.cancel()
        server.close()
        storeA.db.close()
        storeB.db.close()
    }

    private fun refresh(): List<String> = runBlocking {
        requests.clear()
        assertEquals(RefreshOutcome.Ran, repository.refresh())
        requests.toList()
    }

    private val index = "/api/custom-lists"

    @Test
    fun fetchesOnlyChangedLists() = runBlocking {
        assertEquals(listOf(index, "$index/a", "$index/b"), refresh())
        val a = repository.list("a").first().detail!!
        // The gone entry is hidden but counted; the cover is the first with a version.
        assertEquals(listOf("Kept 2"), a.entries.map { it.title })
        assertEquals(2, a.list.entryCount)
        assertEquals("c-a", a.list.cover?.id)

        // Nothing moved: only the index.
        assertEquals(listOf(index), refresh())
        updated["a"] = "u2"
        assertEquals(listOf(index, "$index/a"), refresh())
        // A scan changes entries without moving updated_at: a count or covers that differ fetch the detail.
        counts["a"] = 3
        assertEquals(listOf(index, "$index/a"), refresh())
        assertEquals(listOf("Kept 2", "Kept 3"), repository.list("a").first().detail!!.entries.map { it.title })
        covers["b"] = "w"
        assertEquals(listOf(index, "$index/b"), refresh())
        // A cover change whose detail fails is still fetched by the next refresh.
        covers["a"] = "x"
        errors["a"] = 500 to "test"
        requests.clear()
        assertTrue(repository.refresh() is RefreshOutcome.Failed)
        assertEquals(listOf(index, "$index/a"), requests)
        errors.clear()
        assertEquals(listOf(index, "$index/a"), refresh())
        // Voltis' other refusals keep their message and aren't the server being away.
        for ((code, message) in listOf(404 to "Something else", 403 to "Access denied")) {
            errors["a"] = code to message
            assertEquals(UiText.Raw(message), (repository.refreshList("a") as RefreshOutcome.Failed).text())
        }
        errors.clear()
        assertEquals(0, connectivity.unreachable)
        assertEquals("c-a-2", repository.list("a").first().detail?.entries?.first()?.contentId)
        // Entries over a day old are fetched again.
        clock += 25 * 60 * 60_000L
        assertEquals(listOf(index, "$index/a", "$index/b"), refresh())

        // A detail's 404 deletes the list.
        updated["b"] = "u2"
        errors["b"] = 404 to "List not found"
        assertEquals(listOf(index, "$index/b"), refresh())
        assertNull(repository.list("b").first().detail)
        assertEquals(listOf("a"), repository.lists.first()!!.map { it.id })
        assertEquals(RefreshOutcome.Gone, repository.refreshList("b"))

        // Offline, with a probe that fails too: nothing is asked.
        requests.clear()
        connectivity.online.value = false
        connectivity.reachable = false
        assertEquals(RefreshOutcome.Offline, repository.refresh())
        assertEquals(RefreshOutcome.Offline, repository.refreshList("a"))
        assertEquals(emptyList<String>(), requests)
    }

    @Test
    fun touchesOnlyItsAccount() = runBlocking {
        refresh()
        // Shown, unsubscribed (a hidden page), then removed by the index: a new subscription in the same store says so.
        assertTrue(repository.list("a").first().detail != null)
        val a = updated.remove("a")!!
        refresh()
        assertEquals(CachedList(null, removed = true, open = true), repository.list("a").first())
        updated["a"] = a
        refresh()
        val cached = storeA.db.lists().all()

        // A cached list whose store closes isn't a removal.
        val shown = CompletableDeferred<Unit>()
        val states = scope.async(Dispatchers.Default) {
            repository.list("a").onEach { if (it.detail != null) shown.complete(Unit) }.transformWhile {
                emit(it)
                it.open
            }.toList()
        }
        shown.await()
        current.value = null
        val seen = states.await()
        assertEquals(CachedList(null, removed = false, open = false), seen.last())
        assertTrue(seen.none { it.removed })
        current.value = storeA

        // Signed in to B before A's store closed: the request is refused unsent, which isn't the server's fault.
        signedIn = "server/b"
        updated["a"] = "from-b"
        requests.clear()
        assertEquals(RefreshOutcome.NoStore, repository.refresh())
        assertEquals(emptyList<String>(), requests)
        assertEquals(cached, storeA.db.lists().all())
        assertEquals(0, connectivity.unreachable)

        // An account change while A's index is in flight: stop() cancels and joins it, and nothing reaches A's cache.
        signedIn = "server/a"
        gate = CountDownLatch(1)
        val running = scope.async(Dispatchers.Default) { repository.refresh() }
        assertTrue(arrived.await(10, TimeUnit.SECONDS))
        current.value = null
        repository.stop()
        assertEquals(RefreshOutcome.NoStore, running.await())
        requests.clear()
        gate!!.countDown()
        gate = null
        assertEquals(cached, storeA.db.lists().all())

        // The next store gets a fresh generation.
        current.value = storeB
        signedIn = "server/b"
        assertEquals(RefreshOutcome.Ran, repository.refresh())
        assertEquals(listOf("a", "b"), storeB.db.lists().all().map { it.id }.sorted())
        assertEquals(cached, storeA.db.lists().all())
    }

    @Test
    fun directWrites() = runBlocking {
        counts["a"] = 4
        goneEntries += 3
        refresh()

        // Move steps over the hidden entries, sends every ID, and answers the entry's place in the reloaded cache,
        // found by entry ID: a rename that the reload brings in doesn't lose it.
        fun shown() = runBlocking { repository.list("a").first().detail!!.entries.map { it.entryId } }
        fun entry(n: Int) = EntryItem("c-a-$n", EntryKey("l", "u$n"), "Kept $n", "comic_series", null)
        renamedByReorder = 2
        requests.clear()
        fun revision(list: String) = runBlocking { storeA.db.lists().get(list)!!.revision }
        val first = repository.move("a", entry(2), 1)
        // The place is of the cache revision the reload wrote, which every write of the list's detail bumps.
        assertEquals(Write.Applied(Moved("e2", Place(1, 2), revision("a")), RefreshOutcome.Ran), first)
        assertEquals(listOf("""POST /api/custom-lists/a/entries/reorder {"ctc_ids":["e1","e4","e3","e2"]}"""), writes)
        assertEquals(listOf("$index/a", "$index/a"), requests)
        assertEquals(listOf("e4", "e2"), shown())
        // The hidden entries keep their places.
        assertEquals(listOf("e1", "e4", "e3", "e2"), storeA.db.lists().entriesOf("a").map { it.entryId })
        writes.clear()
        // The way back, by content ID now that the key changed; at the top nothing is sent.
        val back = repository.move("a", entry(2), -1)
        assertEquals(Write.Applied(Moved("e2", Place(0, 2), revision("a")), RefreshOutcome.Ran), back)
        assertTrue(revision("a") > (first as Write.Applied).value!!.revision)
        assertEquals(listOf("e2", "e4"), shown())
        assertEquals(Write.Applied(null, RefreshOutcome.Ran), repository.move("a", entry(2), -1))
        assertEquals(1, writes.size)
        writes.clear()

        // Applied, but its reload failed: the cache is as before, no place is given, and a refresh brings the order in.
        val real = server.dispatcher
        // The reorder lands; every GET after it fails until the errors are cleared.
        server.dispatcher = object : Dispatcher() {
            override fun dispatch(request: RecordedRequest): MockResponse = real.dispatch(request).also {
                if (request.url.encodedPath.endsWith("/reorder")) errors["a"] = 500 to "test"
            }
        }
        val written = repository.move("a", entry(2), 1) as Write.Applied
        assertEquals(Moved("e2", null), written.value)
        assertTrue(written.reload is RefreshOutcome.Failed)
        assertEquals(1, writes.size)
        assertEquals(listOf("e2", "e4"), shown())
        errors.clear()
        val stale = revision("a")
        assertEquals(RefreshOutcome.Ran, repository.refreshList("a"))
        assertEquals(listOf("e4", "e2"), shown())
        assertTrue(revision("a") > stale)
        // A refresh that shows the same rows is a newer state too.
        val same = revision("a")
        assertEquals(RefreshOutcome.Ran, repository.refreshList("a"))
        assertEquals(listOf("e4", "e2"), shown())
        assertTrue(revision("a") > same)
        server.dispatcher = real
        writes.clear()

        // Blank notes for none: nothing to send, and no reload is claimed.
        assertEquals(Write.Applied(Unit, null), repository.setNotes("a", entry(2), " "))
        assertEquals(emptyList<String>(), writes)

        // A blank description for a null one, the name trimmed: nothing to send.
        assertEquals(Write.Applied(false, null), repository.update("a", " List a ", "  ", "private"))
        assertEquals(true, (repository.update("a", "Renamed", " ", "public") as Write.Applied).value)
        assertEquals(listOf("""POST /api/custom-lists/a {"name":"Renamed","description":null,"visibility":"public"}"""), writes)
        writes.clear()

        // The server's count, and the reloaded lists both have the item.
        val item = EntryItem("c-x", EntryKey("l", "u-c-x"), "Made up", "comic_series", null)
        writeAnswers["/entries"] = Answer(body = """{"count": 3}""")
        assertEquals(Write.Applied(Added(3, emptySet()), RefreshOutcome.Ran), repository.addEntries(listOf("a", "b"), listOf(item)))
        assertEquals("""POST /api/custom-lists/entries {"list_ids":["a","b"],"ids":["c-x"]}""", writes.single())
        writes.clear()

        // The entry ID removed.
        val removed = repository.removeEntry("b", item) as Write.Applied
        assertEquals(Removal("x-c-x", storeA.db.lists().get("b")!!.revision), removed.value)
        assertEquals("DELETE /api/custom-lists/b/entries/x-c-x", writes.single())
        // Again: the list has no entry for it, so nothing is sent.
        val again = repository.removeEntry("b", item)
        assertEquals(Write.Applied(Removal(null, storeA.db.lists().get("b")!!.revision), RefreshOutcome.Ran), again)
        assertEquals(1, writes.size)

        // The bulk add's 403 for a deleted list: each list is fetched again, and the gone one is deleted.
        writeAnswers["/entries"] = Answer(403, """{"error": "Not allowed"}""")
        errors["b"] = 404 to "List not found"
        assertEquals(Write.Failed(UiText.Res(R.string.list_gone), RefreshOutcome.Gone), repository.addEntries(listOf("a", "b"), listOf(item)))
        assertNull(repository.list("b").first().detail)
        assertTrue(repository.list("a").first().detail != null)
        writeAnswers.clear()

        // New list with an item: one create, then the add. An add that fails, or one the reload doesn't show, still creates.
        writes.clear()
        val created = repository.create(" Made-up shelf ", null, "private", listOf(item)) as Write.Applied
        assertEquals(Created("n2", "Made-up shelf", null), created.value)
        assertEquals(listOf("c-x"), repository.list("n2").first().detail!!.entries.map { it.contentId })
        writeAnswers["/entries"] = Answer(500, """{"error": "test"}""")
        assertEquals(UiText.Raw("test"), (repository.create("Other shelf", null, "private", listOf(item)) as Write.Applied).value.notAdded)
        writeAnswers["/entries"] = Answer(body = """{"count": 0}""")
        val unknown = EntryItem("unknown-1", EntryKey("l", "u-unknown"), "Made up", "comic_series", null)
        val empty = repository.create("Third shelf", null, "private", listOf(unknown)) as Write.Applied
        assertTrue(empty.value.notAdded != null)
        assertEquals(emptyList<String>(), repository.list(empty.value.id).first().detail!!.entries)
        assertEquals(3, writes.count { it.startsWith("POST /api/custom-lists {") })

        // A delete the server did, whose cache rows can't be removed because the disk filled: applied, with the reload failed,
        // and the row stays for the next refresh. (The cache delete is made to need a page that the file may not grow by.)
        val sql = storeA.db.openHelper.writableDatabase
        sql.execSQL("CREATE TABLE scratch (x BLOB)")
        sql.execSQL("CREATE TRIGGER fill AFTER DELETE ON custom_list BEGIN INSERT INTO scratch VALUES (zeroblob(100000)); END")
        val pages = sql.query("PRAGMA page_count").use { it.moveToFirst(); it.getLong(0) }
        sql.query("PRAGMA max_page_count = $pages").use { it.moveToFirst() }
        val refused = repository.delete("n2") as Write.Applied
        assertTrue((refused.reload as RefreshOutcome.Failed).error is StorageFullException)
        assertTrue(repository.lists.first()!!.any { it.id == "n2" })
        sql.execSQL("DROP TRIGGER fill")
        sql.query("PRAGMA max_page_count = 1073741823").use { it.moveToFirst() }

        // A delete the server already did counts as done.
        writeAnswers["/custom-lists/a"] = Answer(404, """{"error": "List not found"}""")
        assertEquals(Write.Applied(Unit, RefreshOutcome.Ran), repository.delete("a"))
        assertFalse(repository.lists.first()!!.any { it.id == "a" })
    }

    @Test
    fun lostAnswers() = runBlocking {
        goneEntries.clear()
        refresh()
        fun cached() = runBlocking { repository.lists.first()!!.map { it.name } }
        val creates = { writes.count { it.startsWith("POST /api/custom-lists {") } }

        // A create whose answer is lost, or a 5xx after it was stored, may have landed: unknown, never sent
        // again (by OkHttp either), and the refresh after it shows what landed. A 4xx wasn't applied.
        for ((answer, name, outcome) in listOf(
            Triple(Answer(store = true, cut = true), "Cut shelf", Write.Unknown(UiText.Res(R.string.list_create_unknown, "Cut shelf"), RefreshOutcome.Ran)),
            Triple(Answer(500, """{"error": "test"}""", store = true), "Late shelf", Write.Unknown(UiText.Res(R.string.list_create_unknown, "Late shelf"), RefreshOutcome.Ran)),
            Triple(Answer(400, """{"error": "Bad name"}"""), "Refused shelf", Write.Failed(UiText.Raw("Bad name"))),
        )) {
            writes.clear()
            writeAnswers["/custom-lists"] = answer
            assertEquals(outcome, repository.create(name, null, "private"))
            assertEquals(1, creates())
            assertEquals(outcome is Write.Unknown, name in cached())
        }
        // A lost answer relabelled as the missing local network permission says nothing about what was sent.
        writes.clear()
        noLocalNetwork = true
        writeAnswers["/custom-lists"] = Answer(store = true, cut = true)
        assertEquals(Write.Unknown(UiText.Res(R.string.list_create_unknown, "Local shelf"), RefreshOutcome.Ran), repository.create("Local shelf", null, "private"))
        assertEquals(1, creates())
        noLocalNetwork = false
        writeAnswers.clear()

        // A reorder whose answer is lost: unknown, and the reload before the lock is released shows the order that landed.
        writes.clear()
        writeAnswers["/reorder"] = Answer(store = true, cut = true)
        val item = EntryItem("c-a-2", EntryKey("l", "u2"), "Kept 2", "comic_series", null)
        assertEquals(Write.Unknown(UiText.Res(R.string.entry_move_unknown), RefreshOutcome.Ran), repository.move("a", item, -1))
        assertEquals(listOf("e2", "e1"), storeA.db.lists().entriesOf("a").map { it.entryId })
        // Sent once (SendOnce): OkHttp doesn't try again either.
        assertEquals("""POST /api/custom-lists/a/entries/reorder {"ctc_ids":["e2","e1"]}""", writes.single())
        // A reorder the server refuses wasn't applied, and is followed by a reload.
        writeAnswers["/reorder"] = Answer(400, """{"error": "Some entries do not belong to the list"}""")
        assertEquals(Write.Failed(UiText.Raw("Some entries do not belong to the list"), RefreshOutcome.Ran), repository.move("a", item, 1))
        writeAnswers.clear()

        // Two owners moving in one list: the second waits for the first's fetch, write and reload.
        counts["b"] = 3
        refresh()
        val release = CountDownLatch(1)
        writeGate = "/reorder" to release
        fun entry(n: Int) = EntryItem("c-b-$n", EntryKey("l", "u$n"), "Kept $n", "comic_series", null)
        log.clear()
        val first = scope.async(Dispatchers.Default) { repository.move("b", entry(1), 1) }
        assertTrue(writeArrived.await(10, TimeUnit.SECONDS))
        val second = scope.async(Dispatchers.Default) { repository.move("b", entry(3), -1) }
        // Long enough for the second's GET to show if it didn't wait.
        Thread.sleep(300)
        release.countDown()
        assertTrue(first.await() is Write.Applied)
        assertTrue(second.await() is Write.Applied)
        val get = "GET $index/b"
        val reorder = "POST $index/b/entries/reorder"
        assertEquals(listOf(get, reorder, get, get, reorder, get), log)
        assertEquals(listOf("e2", "e3", "e1"), storeA.db.lists().entriesOf("b").map { it.entryId })
    }

    @Test
    fun teardownAfterSend() = runBlocking {
        refresh()
        // The account goes while a create is held after it was sent: it may have landed, so not Failed, which says repeat it.
        val release = CountDownLatch(1)
        writeGate = "/custom-lists" to release
        val held = scope.async(Dispatchers.Default) { repository.create("Held shelf", null, "private") }
        assertTrue(writeArrived.await(10, TimeUnit.SECONDS))
        repository.stop()
        release.countDown()
        assertEquals(Write.Unknown(UiText.Res(R.string.list_create_unknown, "Held shelf"), RefreshOutcome.NoStore), held.await())
    }
}
