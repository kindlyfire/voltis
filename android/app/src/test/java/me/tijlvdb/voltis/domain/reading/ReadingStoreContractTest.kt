package me.tijlvdb.voltis.domain.reading

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.downloads.cache
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.RequestedBy
import me.tijlvdb.voltis.data.reading.RoomReadingStore
import me.tijlvdb.voltis.domain.sync.NoticeDetail
import me.tijlvdb.voltis.domain.sync.NoticeKind
import me.tijlvdb.voltis.domain.sync.StoredNotice
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** What the engine relies on from a [ReadingStore], run against both. */
abstract class ReadingStoreContractTest {
    protected val announced = mutableListOf<StoredNotice>()

    abstract fun store(): ReadingStore

    /** Caches content rows with their positions, as `DownloadRepository` does. */
    abstract suspend fun cache(rows: List<Pair<Content, Int?>>, wholeList: String? = null)

    /** Gives [id] a download row. */
    abstract suspend fun download(id: String)

    /** A catalog refresh dropping the rows of [ids] that nothing owns (no download, no op of their own). */
    abstract suspend fun uncache(ids: List<String>)

    private fun page(n: Int) = JsonObject(mapOf("current_page" to JsonPrimitive(n)))

    private val notice = StoredNotice(0, "a", "Alpha", NoticeKind.REFUSED, NoticeDetail(message = "No"), 5)

    @Test
    fun commitIsAllOrNothing() = runBlocking {
        val store = store()
        val x = Lane("x", title = "Kept")
        val y = Lane("y", title = "Kept too")
        val ids = store.commit(
            Change(lanes = listOf(x, y), ops = listOf(Op(contentId = "x", kind = OpKind.POSITION, payload = page(1)), Op(contentId = "x", kind = OpKind.FINISH, payload = page(9)))),
        )
        val before = store.load()

        // Deletions and updates of existing rows fail with the update of an op that doesn't exist.
        val failed = runCatching {
            store.commit(
                Change(
                    lanes = listOf(Lane("a"), x.copy(title = "Changed")),
                    deleteLanes = listOf("y"),
                    ops = listOf(
                        Op(contentId = "a", kind = OpKind.POSITION, payload = page(1)),
                        Op(id = ids[1], contentId = "x", kind = OpKind.FINISH, payload = page(9), sentSeq = 3),
                        Op(id = 99, contentId = "x", kind = OpKind.FINISH, payload = page(9)),
                    ),
                    deleteOps = listOf(ids[0]),
                    notices = listOf(notice),
                    snapshots = listOf(ReadingSnapshot("a", ReadingState(seq = 5))),
                ),
            )
        }
        assertTrue(failed.isFailure)
        assertEquals(before, store.load())
        // The states of an answer are stored with its commit, or not at all.
        assertNull(store.snapshot("a"))
        assertEquals(listOf(x, y, null), listOf("x", "y", "a").map { store.lane(it) })
        assertEquals(emptyList<StoredNotice>(), announced)
    }

    @Test
    fun opsComeBackInOrderWithTheirLanes() = runBlocking {
        val store = store()
        // What the flows emit, each change once.
        val unsent = MutableStateFlow<Int?>(null)
        val shown = MutableStateFlow<Map<String, Shown>?>(null)
        val unsentSeen = mutableListOf<Int>()
        val shownSeen = mutableListOf<Boolean?>()
        val collectors = launch {
            launch { store.unsent().collect { unsent.value = it.also { if (unsentSeen.lastOrNull() != it) unsentSeen += it } } }
            launch {
                store.shown(setOf("a", "x")).collect {
                    shown.value = it
                    val flag = it["a"]?.unsent
                    if (shownSeen.isEmpty() || shownSeen.last() != flag) shownSeen += flag
                }
            }
        }
        suspend fun settled(count: Int, aUnsent: Boolean?) = withTimeout(5_000) {
            unsent.first { it == count }
            shown.first { it != null && it["a"]?.unsent == aUnsent }
        }
        settled(0, null)

        val series = SeriesInfo("s", "reading", "srv:1", childrenCount = 2)
        val a = Lane("a", parentId = "s", title = "Alpha", pageCount = 10, state = ReadingState("srv:2", "reading", progress = page(3)), series = series, here = page(4), shownStatus = "completed", touchedAt = 100)
        val held = Lane("b", needsReview = true, foreignEpoch = 2, touchedAt = 100)
        val idle = Lane("c", touchedAt = 100)
        val ids = store.commit(
            Change(
                lanes = listOf(a, held, idle, Lane("d")),
                ops = listOf(
                    Op(contentId = "a", kind = OpKind.POSITION, payload = page(4)),
                    Op(contentId = "a", kind = OpKind.COMMAND, payload = JsonObject(mapOf("op" to JsonPrimitive("set_status"))), after = listOf("a")),
                ),
                notices = listOf(notice),
            ),
        )
        assertEquals(2, ids.size)
        assertTrue(ids[0] < ids[1])
        assertEquals(listOf("Alpha"), announced.map { it.title })
        assertTrue(announced.single().id > 0)
        settled(2, true)
        // What is shown differs from what was acknowledged: projected.
        assertEquals(mapOf("a" to Shown("completed", page(3), null, unsent = true, needsReview = false, projected = true)), shown.value)
        // Awaiting review, by series and title, else by ID; named with the series' lane's title.
        store.commit(Change(lanes = listOf(Lane("e", parentId = "p", title = "Echo", needsReview = true), Lane("p", title = "Pike"))))
        assertEquals(listOf(AttentionItem.Held("b", "b"), AttentionItem.Held("e", "Pike · Echo")), store.held().first())
        assertEquals("Pike", store.title("p"))
        store.commit(Change(deleteLanes = listOf("e", "p")))

        // Sent, as an answer leaves it: the first op updated, the second deleted, a lane deleted.
        val first = Op(
            id = ids[0], contentId = "a", kind = OpKind.POSITION, payload = page(4), sealed = true, after = listOf("b"), sentSeq = 7,
            sentWriter = "tcontracttest0000", epoch = 3, guard = JsonObject(mapOf("series" to JsonPrimitive("srv:1"))),
            attempts = 2, lastError = "test", createdAt = 50,
        )
        store.commit(Change(ops = listOf(first), deleteOps = listOf(ids[1]), deleteLanes = listOf("d")))
        settled(1, true)
        val third = store.commit(Change(ops = listOf(Op(contentId = "b", kind = OpKind.FINISH, payload = page(9))))).single()
        settled(2, true)

        val stored = store.load()
        assertEquals(listOf(ids[0], third), stored.ops.map { it.id })
        assertEquals(first, stored.ops[0])
        // An op without `after` comes back with an empty one.
        assertEquals(emptyList<String>(), stored.ops[1].after)
        // The lanes of the ops and those awaiting review; not an idle one.
        assertEquals(mapOf("a" to a, "b" to held), stored.lanes)
        assertEquals(idle, store.lane("c"))
        assertNull(store.lane("d"))

        store.commit(Change(deleteOps = listOf(ids[0])))
        settled(1, false)
        collectors.cancel()
        assertEquals(listOf(0, 2, 1, 2, 1), unsentSeen)
        assertEquals(listOf(null, true, false), shownSeen)

        // Pruning keeps lanes with ops or awaiting review.
        store.prune(before = 200)
        assertEquals(listOf(null, held, null), listOf("a", "b", "c").map { store.lane(it) })
    }

    @Test
    fun cachedRows() = runBlocking {
        val store = store()
        assertNull(store.volumes("s"))
        fun volume(id: String, userData: UserData? = null, valid: Boolean = true) =
            Content(id, id.uppercase(), ContentType.COMIC, parentId = "s", valid = valid, userData = userData)
        val fetched = UserData(status = "reading", progress = page(2), revision = "srv:a", readingSeq = 4)
        val rows = listOf(
            Content("s", "Series", ContentType.COMIC_SERIES, userData = UserData(status = "reading", revision = "srv:s", readingSeq = 3)) to null,
            volume("b") to 1,
            volume("a", fetched) to 0,
            volume("x", valid = false) to 2,
        )
        // Rows alone say nothing of the list: it is known once it was cached whole.
        cache(rows)
        assertNull(store.volumes("s"))
        cache(rows, wholeList = "s")
        // An untouched volume is the empty state at seq 0, not an unknown one; the guard reads the snapshots.
        val volumes = SeriesVolumes("srv:s", listOf(VolumeRef("a", fetched), VolumeRef("b", ReadingState().userData())))
        assertEquals(volumes, store.volumes("s"))
        // A newer answer for a volume is what the guard sees, and a lane that is older changes nothing of it.
        store.commit(Change(snapshots = listOf(ReadingSnapshot("b", ReadingState("tcontract:5", "completed", seq = 9))), lanes = listOf(Lane("b", state = ReadingState("t:1", "reading", seq = 2)))))
        assertEquals("completed", store.volumes("s")?.volumes?.last()?.userData?.status)
        // A valid volume without a position: the order isn't known, so the series counts as not cached.
        cache(listOf(volume("c") to null))
        assertNull(store.volumes("s"))

        // Pruning keeps a lane with a download; a snapshot lives with its content row, download, op or review, not with its lane.
        store.commit(Change(lanes = listOf(Lane("d", touchedAt = 1), Lane("e", touchedAt = 1)), snapshots = listOf("d", "e", "f", "g").map { ReadingSnapshot(it, ReadingState(seq = 1)) }))
        download("d")
        store.commit(Change(lanes = listOf(Lane("g", needsReview = true))))
        store.prune(before = 100)
        assertEquals(listOf(true, false), listOf("d", "e").map { store.lane(it) != null })
        assertEquals(listOf(true, false, false, true, true), listOf("d", "e", "f", "g", "b").map { store.snapshot(it) != null })
    }

    /** The primitive keeps the greatest seq of each content, whatever order, repeats and restarts bring them, and times that run backwards. */
    @Test
    fun importKeepsTheGreatestSeq() = runBlocking {
        val early = "2026-01-01T00:00:00Z"
        val late = "2026-03-01T00:00:00Z"
        val one = ReadingState("srv:1", "reading", late, page(3), late, late, seq = 1)
        // A clear: no times at all, and a higher seq.
        val two = ReadingState("srv:2", seq = 2)
        // Written later, but the device that wrote it had its clock behind: the times are older.
        val three = ReadingState("srv:3", "completed", early, page(9), early, early, seq = 3)
        val states = listOf(one, two, three)
        val orders = listOf(listOf(0, 1, 2), listOf(0, 2, 1), listOf(1, 0, 2), listOf(1, 2, 0), listOf(2, 0, 1), listOf(2, 1, 0))
        for ((i, order) in orders.withIndex()) {
            val id = "c$i"
            // A restart in the middle: a store on the same data.
            store().import(listOf(ReadingSnapshot(id, states[order[0]])))
            store().import(listOf(ReadingSnapshot(id, states[order[1]]), ReadingSnapshot(id, states[order[0]])))
            store().import(listOf(ReadingSnapshot(id, states[order[2]])))
            assertEquals(three, store().snapshot(id))
        }
        // Seq 0 is a content nobody touched: it fills an absence and never replaces; an equal seq replaces nothing even when the status differs (by design: it is ignored, not an error).
        val store = store()
        store.import(listOf(ReadingSnapshot("n", ReadingState())))
        assertEquals(ReadingState(), store.snapshot("n"))
        store.import(listOf(ReadingSnapshot("n", one)))
        store.import(listOf(ReadingSnapshot("n", ReadingState()), ReadingSnapshot("n", one.copy(status = "dropped"))))
        assertEquals(one, store.snapshot("n"))
        assertNull(store.snapshot("none"))
    }

    /** What a lane holds is its sync base; the effective reading is the snapshot with the unsent ops over it, in order. */
    @Test
    fun effectiveIsTheSnapshotWithTheOutboxOverIt() = runBlocking {
        val store = store()
        val t = "2026-01-01T00:00:00Z"
        // Another device moved the item to on hold, and the phone learned it; the lane's base, held for review, is older.
        val lane = Lane("a", parentId = "s", type = ContentType.COMIC, pageCount = 10, state = ReadingState("srv:1", "completed", t, page(9), t, t, seq = 1), needsReview = true)
        store.commit(Change(snapshots = listOf(ReadingSnapshot("a", ReadingState("srv:5", "on_hold", t, page(2), t, t, seq = 5)), ReadingSnapshot("b", ReadingState(seq = 0))), lanes = listOf(lane)))
        assertEquals(EffectiveReading("on_hold", page(2), t, Stamps(t, null, t, null), seq = 5, revision = "srv:5", progressUpdatedAt = t), store.effective(listOf("a"))["a"])

        store.commit(Change(ops = listOf(Op(contentId = "a", kind = OpKind.POSITION, payload = page(4), createdAt = 1000))))
        val effective = store.effective(listOf("a", "b", "unknown"))
        // On the snapshot a position moves the held item to Reading, which stamps both times by queue place; on the lane's completed base it wouldn't.
        assertEquals(EffectiveReading("reading", page(4), "1970-01-01T00:00:01Z", Stamps(null, 0, null, 0), seq = 5, pending = true, revision = "srv:5", progressUpdatedAt = t), effective["a"])
        assertEquals(setOf("a", "b"), effective.keys)
        assertEquals(EffectiveReading(null, EmptyProgress, null, Stamps(null, null, null, null), seq = 0), effective["b"])
    }

    /** What an op projects as nothing stays nothing: the snapshot's old status and times don't show through a clear, a status removal or a series clear. */
    @Test
    fun effectiveKeepsWhatAnOpClears() = runBlocking {
        val store = store()
        val t = "2026-01-01T00:00:00Z"
        val done = ReadingState("srv:7", "completed", t, page(9), t, t, seq = 7)
        fun command(name: String, vararg fields: Pair<String, JsonPrimitive>) = JsonObject(mapOf("op" to JsonPrimitive(name)) + fields)
        val guard = JsonObject(mapOf("series" to JsonNull, "volumes" to JsonObject(mapOf("v" to JsonNull, "w" to JsonNull))))
        store.commit(
            Change(
                snapshots = listOf("x", "y", "s", "v", "w").map { ReadingSnapshot(it, done) },
                lanes = listOf("x", "y", "v", "w").map { Lane(it, parentId = "s", type = ContentType.COMIC, pageCount = 10) } + Lane("s", type = ContentType.COMIC_SERIES),
                ops = listOf(
                    Op(contentId = "x", kind = OpKind.COMMAND, payload = command("clear")),
                    Op(contentId = "y", kind = OpKind.COMMAND, payload = command("set_status", "status" to JsonPrimitive(null as String?))),
                    Op(contentId = "s", kind = OpKind.COMMAND, payload = command("clear"), guard = guard),
                ),
            ),
        )
        val effective = store.effective(listOf("x", "y", "v", "w"))
        // A clear: no status, no position, no times; a status removal keeps the position and the read time.
        assertEquals(EffectiveReading(null, EmptyProgress, null, Stamps(null, null, null, null), seq = 7, pending = true, revision = "srv:7", progressUpdatedAt = t), effective["x"])
        assertEquals(listOf(null, page(9), t), effective.getValue("y").let { listOf(it.status, it.progress, it.lastReadAt) })
        // The series clear reaches the volumes its guard covers.
        for (id in listOf("v", "w")) assertEquals(EffectiveReading(null, EmptyProgress, null, Stamps(null, null, null, null), seq = 7, pending = true, revision = "srv:7", progressUpdatedAt = t), effective[id])
    }

    /** An unsent op keeps what it reads and writes, whatever becomes of the content rows and lanes: its guarded children's snapshots and lanes, and its series'. */
    @Test
    fun anUnsentOpKeepsWhatItGuards() = runBlocking {
        val t = "2026-01-01T00:00:00Z"
        fun volume(id: String) = Content(id, id.uppercase(), ContentType.COMIC, parentId = "s", userData = UserData(status = "reading", progress = page(2), revision = "srv:4", readingSeq = 4, lastReadAt = t))
        val rows = listOf(Content("s", "Series", ContentType.COMIC_SERIES, userData = UserData(status = "reading", revision = "srv:3", readingSeq = 3)) to null, volume("a") to 0, volume("b") to 1, volume("c") to 2)
        cache(rows, wholeList = "s")
        val guard = JsonObject(mapOf("series" to JsonPrimitive("srv:3"), "volumes" to JsonObject(mapOf("a" to JsonNull, "b" to JsonNull))))
        store().commit(
            Change(
                lanes = listOf(Lane("s", type = ContentType.COMIC_SERIES, touchedAt = 1), Lane("a", parentId = "s", type = ContentType.COMIC, pageCount = 10, touchedAt = 1), Lane("c", touchedAt = 1)),
                ops = listOf(Op(contentId = "s", kind = OpKind.COMMAND, payload = JsonObject(mapOf("op" to JsonPrimitive("clear"))), guard = guard)),
            ),
        )
        // Two volumes left the series' list and their rows went with the refresh; one of them was never guarded.
        uncache(listOf("b", "c"))
        // A restart, and the prune after it.
        val restarted = store()
        restarted.load()
        restarted.prune(before = 100)
        // Snapshots: the guarded ones stay without a row; the unguarded one goes. A lane the guard covers stays; an idle one doesn't.
        assertEquals(listOf(true, true, false), listOf("a", "b", "c").map { restarted.snapshot(it) != null })
        assertEquals(listOf(true, false), listOf("a", "c").map { restarted.lane(it) != null })
        // An answer older than the one the guard was made against arrives late: it takes nothing back, and the clear still shows.
        restarted.import(listOf(ReadingSnapshot("b", ReadingState("srv:1", "dropped", seq = 1))))
        assertEquals(4L, restarted.snapshot("b")?.seq)
        assertEquals(listOf(null, null, 4L), restarted.effective(listOf("b")).getValue("b").let { listOf(it.status, it.lastReadAt, it.seq) })
    }
}

class InMemoryReadingStoreTest : ReadingStoreContractTest() {
    private val store = InMemoryReadingStore { announced += it }

    override fun store() = store

    override suspend fun cache(rows: List<Pair<Content, Int?>>, wholeList: String?) = store.cache(rows, wholeList)

    override suspend fun download(id: String) {
        store.downloads += id
    }

    override suspend fun uncache(ids: List<String>) = store.uncache(ids)
}

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [37], application = Application::class)
class RoomReadingStoreTest : ReadingStoreContractTest() {
    private lateinit var db: VoltisDatabase

    @Before
    fun open() {
        db = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), VoltisDatabase::class.java).build()
    }

    @After
    fun close() = db.close()

    override fun store() = RoomReadingStore(db) { announced += it }

    override suspend fun cache(rows: List<Pair<Content, Int?>>, wholeList: String?) {
        db.cache(rows, fetchedAt = 1) { false }
        wholeList?.let { db.content().setVolumesKnown(it, true) }
    }

    override suspend fun uncache(ids: List<String>) = db.content().deleteUnneeded(ids)

    override suspend fun download(id: String) {
        db.downloads().insert(listOf(DownloadEntity(id, id, DownloadState.DONE, requestedBy = RequestedBy.USER, queuedAt = 1, copyId = "copy-$id")))
    }
}
