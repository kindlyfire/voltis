package me.tijlvdb.voltis.data.db

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import androidx.room.withTransaction
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.CoverRef
import me.tijlvdb.voltis.data.api.CustomListDetail
import me.tijlvdb.voltis.data.api.CustomListEntry
import me.tijlvdb.voltis.data.api.CustomListSummary
import me.tijlvdb.voltis.data.api.ListVisibility
import me.tijlvdb.voltis.data.api.DisplayMetadata
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.downloads.DownloadStore
import me.tijlvdb.voltis.data.downloads.NewDownload
import me.tijlvdb.voltis.data.downloads.cache
import me.tijlvdb.voltis.data.downloads.patchUserData
import me.tijlvdb.voltis.data.lists.coverRefs
import me.tijlvdb.voltis.data.lists.deleteLists
import me.tijlvdb.voltis.data.lists.storeIndex
import me.tijlvdb.voltis.data.lists.storeList
import me.tijlvdb.voltis.data.sync.RoomPendingStore
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.domain.sync.LandedUserData
import me.tijlvdb.voltis.domain.sync.NoticeDetail
import me.tijlvdb.voltis.domain.sync.NoticeKind
import me.tijlvdb.voltis.domain.sync.PendingChange
import me.tijlvdb.voltis.domain.sync.StoredNotice
import me.tijlvdb.voltis.domain.sync.UserDataItem
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.RequestedBy
import me.tijlvdb.voltis.domain.reading.OpKind
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [37], application = Application::class)
class DaoTest {
    @get:Rule
    val temp = TemporaryFolder()

    private val db = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), VoltisDatabase::class.java).build()
    private val downloads = db.downloads()

    @After
    fun close() = db.close()

    private fun row(id: String, series: String = id, state: String = DownloadState.QUEUED, queuedAt: Long = 10) =
        DownloadEntity(id, series, state, requestedBy = RequestedBy.USER, queuedAt = queuedAt, transferId = "t-$id")

    private fun volume(id: String, series: String, userData: UserData? = null) = Content(id, id, ContentType.COMIC, parentId = series, userData = userData)

    @Test
    fun queue() = runBlocking {
        downloads.insert(listOf(row("b", "s", queuedAt = 20), row("a", "s"), row("c", "s"), row("r", "s", DownloadState.RUNNING, queuedAt = 5)))
        // An existing row is left as it is.
        downloads.insert(listOf(row("a", "s", DownloadState.PAUSED)))
        assertEquals(DownloadState.QUEUED, downloads.get("a")?.state)
        assertTrue(downloads.references("t-a"))

        // Oldest first, then in insertion order; a running row isn't taken.
        val order = mutableListOf<String>()
        while (true) {
            val next = downloads.next() ?: break
            order += next.contentId
            downloads.delete(listOf(next.contentId))
        }
        assertEquals(listOf("a", "c", "b"), order)
        assertEquals(listOf("r"), downloads.running().map { it.contentId })
    }

    @Test
    fun contentRows() = runBlocking {
        val content = db.content()
        // Started first: its start prunes the content nothing owns.
        val scope = CoroutineScope(SupervisorJob())
        val store = DownloadStore(AccountStore("a", temp.root, db), scope, {})
        // s1 has downloads; s2 has a lane with an unsent op; s3 has neither.
        db.cache(
            listOf("s1", "s2", "s3").flatMap { s -> listOf(Content(s, s, ContentType.COMIC_SERIES) to null, volume("$s-v1", s) to 0, volume("$s-v2", s) to 1) },
            fetchedAt = System.currentTimeMillis(),
        ) { false }
        db.lanes().upsert(listOf(laneOf("s2-v2", "s2")))
        db.ops().insert(listOf(OpEntity(0, "s2-v2", OpKind.POSITION, "{}", false, null, null, null, null, null, 0, null, 0)))
        // A list row doesn't replace a detail row's json.
        val detail = volume("s1-v1", "s1").copy(meta = DisplayMetadata(description = "Kept"))
        db.cache(listOf(detail to null), fetchedAt = 2) { true }
        db.cache(listOf(volume("s1-v1", "s1").copy(title = "Renamed") to 0), fetchedAt = 3) { false }
        val cached = content.get("s1-v1")!!
        assertEquals("Renamed" to 0, cached.title to cached.position)
        assertEquals("Kept", AppJson.decodeFromString(Content.serializer(), cached.json).meta.description)

        // Deleting the last downloads drops the rows of a series nothing needs, unless one of its lanes has something unsent.
        try {
            store.enqueue(listOf(NewDownload("s1-v1", "s1", RequestedBy.USER), NewDownload("s1-v2", "s1", RequestedBy.USER)))
            store.delete(listOf("s1-v1"))
            assertEquals(listOf("s1", "s1-v1", "s1-v2"), content.get(listOf("s1", "s1-v1", "s1-v2")).map { it.id }.sorted())
            store.deleteAll()
            // Only the series whose rows went are pruned: s3's, possibly a queueing in flight, stay.
            assertEquals(listOf("s2", "s2-v1", "s2-v2", "s3", "s3-v1"), content.get(listOf("s1", "s1-v1", "s2", "s2-v1", "s2-v2", "s3", "s3-v1")).map { it.id }.sorted())
        } finally {
            store.stop()
            scope.cancel()
        }
    }

    @Test
    fun aListRowCachedDuringADetailWriteKeepsIt() = runBlocking {
        val detail = volume("v", "s").copy(meta = DisplayMetadata(description = "Detail"))
        val written = CompletableDeferred<Unit>()
        val release = CompletableDeferred<Unit>()
        // The detail row is written in a transaction that stays open while the list row's caching starts.
        val writer = launch(Dispatchers.IO) {
            db.withTransaction {
                db.cache(listOf(detail to null), fetchedAt = 2) { true }
                written.complete(Unit)
                release.await()
            }
        }
        written.await()
        // Undispatched: this returns with the list row's transaction asked for, behind the open one.
        val listed = launch(Dispatchers.IO, CoroutineStart.UNDISPATCHED) { db.cache(listOf(volume("v", "s").copy(title = "Listed") to 3), fetchedAt = 3) { false } }
        release.complete(Unit)
        writer.join()
        listed.join()
        val row = db.content().get("v")!!
        assertEquals(listOf<Any?>("Listed", 3, true), listOf(row.title, row.position, row.detail))
        assertEquals("Detail", AppJson.decodeFromString(Content.serializer(), row.json).meta.description)
    }

    @Test
    fun patchUserData() = runBlocking {
        db.cache(listOf(volume("a", "s") to 0, volume("b", "s", UserData(status = "reading", rating = 4)) to 1), fetchedAt = 1) { false }
        fun userData(id: String) = runBlocking { AppJson.decodeFromString(Content.serializer(), db.content().get(id)!!.json).userData }

        db.patchUserData("a", starred = true, rating = null)
        assertEquals(UserData(starred = true), userData("a"))
        db.patchUserData("b", starred = null, rating = JsonNull)
        assertEquals(UserData(status = "reading"), userData("b"))
        db.patchUserData("b", starred = null, rating = JsonPrimitive(2))
        assertEquals(2, userData("b")?.rating)
        // No cached row: nothing happens.
        db.patchUserData("missing", starred = true, rating = null)
        assertNull(db.content().get("missing"))
    }

    /** Star and rating rows (P4 §7): merged, landed at the sent `rev` only, kept for their landed values until the owner's start. */
    @Test
    fun pendingUserData() = runBlocking {
        db.cache(listOf(volume("a", "s") to 0), fetchedAt = 1) { false }
        val store = RoomPendingStore(db)
        val item = UserDataItem("a", "l", "u", "Vol. 1")
        fun cached() = runBlocking { AppJson.decodeFromString(Content.serializer(), db.content().get("a")!!.json).userData }

        store.commit(PendingChange.Submit(item, true, null, now = 5))
        store.commit(PendingChange.Submit(item, null, JsonNull, now = 6))
        var row = store.wanted().single()
        assertEquals(listOf(2L, true, true, null, 5L), listOf(row.rev, row.starred, row.ratingSet, row.rating, row.createdAt))

        // A failure counts at the sent rev only; a landing of an older rev keeps the newer wish, and resets its attempts.
        store.commit(PendingChange.Failed("a", 2, "down"))
        store.commit(PendingChange.Failed("a", 1, "stale"))
        assertEquals(1, store.wanted().single().attempts)
        store.commit(PendingChange.Landed("a", 1, UserData(starred = true)))
        row = store.wanted().single()
        assertEquals(listOf(0, true, LandedUserData(1, true, null)), listOf(row.attempts, row.starred, row.landed))
        assertEquals(true, cached()?.starred)
        // At the sent rev the wish goes, and the row stays with the landed values, with the next sequence number.
        // Its receipt carries the item's whole reading state, which another device may have changed: imported with the landing.
        store.commit(PendingChange.Landed("a", 2, UserData(starred = true, rating = 3, status = "dropped", revision = "srv:6", readingSeq = 6)))
        assertEquals(6L to "dropped", db.snapshots().get("a")?.let { it.seq to it.status })
        assertEquals(emptyList<Any>(), store.wanted())
        assertEquals(LandedUserData(2, true, 3), store.watch("a").first()?.landed)
        assertEquals(3, cached()?.rating)
        assertEquals(2L, store.landedSeq())

        // Dropped and Discard clear the wish only: a row without landed values is deleted, one with them stays. A moved rev drops nothing.
        val other = UserDataItem("b", "l", "v", "Vol. 2")
        val notice = StoredNotice(0, "b", "Vol. 2", NoticeKind.GONE, NoticeDetail(), 1)
        store.commit(PendingChange.Submit(other, true, null, now = 7))
        store.commit(PendingChange.Submit(other, true, null, now = 8))
        assertEquals(emptyList<StoredNotice>(), store.commit(PendingChange.Dropped("b", 1, notice)))
        assertEquals(1, store.commit(PendingChange.Dropped("b", 2, notice)).size)
        assertNull(store.watch("b").first())
        assertEquals(1, db.notices().observe().first().size)
        store.commit(PendingChange.Submit(item, false, null, now = 9))
        store.commit(PendingChange.Discard("a"))
        assertEquals(LandedUserData(2, true, 3), store.watch("a").first()?.landed)

        // A refused commit (a full disk) leaves the rows and the cached content as they were.
        store.commit(PendingChange.Submit(item, false, null, now = 10))
        val sql = db.openHelper.writableDatabase
        sql.execSQL("CREATE TABLE scratch (x BLOB)")
        sql.execSQL("CREATE TRIGGER fill AFTER UPDATE ON pending_user_data BEGIN INSERT INTO scratch VALUES (zeroblob(100000)); END")
        val pages = sql.query("PRAGMA page_count").use { it.moveToFirst(); it.getLong(0) }
        sql.query("PRAGMA max_page_count = $pages").use { it.moveToFirst() }
        val before = store.watch("a").first()
        try {
            store.commit(PendingChange.Landed("a", before!!.rev, UserData(starred = false, rating = 1, status = "completed", readingSeq = 9)))
            error("expected a refusal")
        } catch (_: StorageFullException) {
        }
        sql.execSQL("DROP TRIGGER fill")
        sql.query("PRAGMA max_page_count = 1073741823").use { it.moveToFirst() }
        assertEquals(listOf(before, 3), listOf(store.watch("a").first(), cached()?.rating))
        // The state imported with a landing is refused with it.
        assertEquals(6L, db.snapshots().get("a")?.seq)

        // The owner's start: rows without a wish go, the landed values of the rest are cleared.
        store.commit(PendingChange.Landed("a", before!!.rev, UserData(starred = false)))
        store.commit(PendingChange.Submit(other, true, null, now = 11))
        store.commit(PendingChange.Landed("b", 1, UserData(starred = true)))
        store.commit(PendingChange.Submit(other, false, null, now = 12))
        store.prune()
        assertEquals(listOf("b"), store.wanted().map { it.contentId })
        assertEquals(null, store.watch("b").first()?.landed)
        assertNull(store.watch("a").first())
        assertEquals(0L, store.landedSeq())
    }

    @Test
    fun listsCache() = runBlocking {
        val lists = db.lists()
        fun summary(id: String, updated: String, covers: List<CoverRef> = emptyList()) = CustomListSummary(id, updated, "List $id", null, ListVisibility.PRIVATE, 1, covers)
        fun detail(id: String, updated: String, vararg uris: String) = CustomListDetail(
            id, updated, "List $id", null, ListVisibility.PRIVATE, uris.size,
            uris.map { CustomListEntry("e-$it", "l", it, Content("c-$it", "Title $it", ContentType.COMIC_SERIES).takeIf { _ -> it != "gone" }) },
        )
        db.storeIndex(listOf(summary("a", "u1", listOf(CoverRef("c-x", "v1"))), summary("b", "u1")), fetchedAt = 1)
        db.storeList(detail("a", "u1", "x", "gone"), fetchedAt = 2)
        db.storeList(detail("b", "u1", "y"), fetchedAt = 2)
        val entries = lists.entries("a").first()
        assertEquals(listOf("x" to "c-x", "gone" to null), entries.map { it.uri to it.contentId })
        // A gone content's title is its uri.
        assertEquals("gone", entries[1].title)

        // An index without b names it for deletion: b goes with its entries; a keeps the time its entries were fetched.
        val gone = db.storeIndex(listOf(summary("a", "u2", listOf(CoverRef("c-z", "v2")))), fetchedAt = 3)
        assertEquals(listOf("b"), gone)
        db.deleteLists(gone)
        assertEquals(listOf("a"), lists.all().map { it.id })
        assertEquals(emptyList<CustomListEntryEntity>(), lists.entries("b").first())
        assertEquals(listOf("u2", "u1"), lists.get("a")!!.let { listOf(it.updatedAt, it.entriesUpdatedAt) })

        // A detail replaces every entry, and keeps the index's covers.
        db.storeList(detail("a", "u3", "z"), fetchedAt = 4)
        assertEquals(listOf("z"), lists.entries("a").first().map { it.uri })
        val a = lists.get("a")!!
        assertEquals(listOf("u3", "u3"), listOf(a.updatedAt, a.entriesUpdatedAt))
        assertEquals(4L, a.detailFetchedAt)
        assertEquals(listOf(CoverRef("c-z", "v2")), a.coverRefs())
    }

    private fun laneOf(id: String, parent: String) = LaneEntity(
        id, null, null, parent, ContentType.COMIC, id, null, true, false, null, null, null, "{}", null, null, 0, 0, null, null,
        true, false, 0, false, null, "{}", null, 0,
    )
}
