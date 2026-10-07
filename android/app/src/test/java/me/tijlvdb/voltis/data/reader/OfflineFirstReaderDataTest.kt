package me.tijlvdb.voltis.data.reader

import android.app.Application
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import java.io.File
import java.io.FileNotFoundException
import java.io.IOException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import me.tijlvdb.voltis.data.api.AccountChangedException
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.db.AccountStore
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.downloads.CopyMade
import me.tijlvdb.voltis.data.downloads.DownloadStore
import me.tijlvdb.voltis.data.downloads.MANIFEST
import me.tijlvdb.voltis.data.downloads.NewDownload
import me.tijlvdb.voltis.data.downloads.Observation
import me.tijlvdb.voltis.data.downloads.OfflineManifest
import me.tijlvdb.voltis.data.downloads.OfflinePage
import me.tijlvdb.voltis.data.downloads.PinKey
import me.tijlvdb.voltis.data.downloads.Pins
import me.tijlvdb.voltis.data.downloads.cache
import me.tijlvdb.voltis.domain.comic.FakeReaderData
import me.tijlvdb.voltis.domain.comic.OpenedComic
import me.tijlvdb.voltis.domain.comic.PageDimensions
import me.tijlvdb.voltis.domain.comic.ReaderData
import me.tijlvdb.voltis.domain.comic.volume
import me.tijlvdb.voltis.domain.downloads.RequestedBy
import me.tijlvdb.voltis.domain.downloads.ServerFile
import me.tijlvdb.voltis.domain.downloads.PageDamaged
import me.tijlvdb.voltis.domain.downloads.Stale
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.storage.StorageFullException
import me.tijlvdb.voltis.domain.reading.FakeReadingSync
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import retrofit2.HttpException
import retrofit2.Response

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [37], application = Application::class)
class OfflineFirstReaderDataTest {
    @get:Rule
    val temp = TemporaryFolder()

    private val db = Room.inMemoryDatabaseBuilder(ApplicationProvider.getApplicationContext(), VoltisDatabase::class.java).build()
    private val scope = CoroutineScope(SupervisorJob())

    @After
    fun close() {
        scope.cancel()
        db.close()
    }

    private class Net(private val data: FakeReaderData) : AccountReaderData, ReaderData by data {
        /** Every open fails with it, when set. */
        var failure: Exception? = null
        val accounts = mutableListOf<String?>()

        override suspend fun open(contentId: String, account: ForAccount?): OpenedComic {
            accounts += account?.account
            failure?.let { throw it }
            return data.open(contentId, CoroutineScope(Job()))
        }
    }

    private class Conn : Connectivity {
        private val state = MutableStateFlow(true)

        /** Runs as the reader asks whether it is online: after its pin, before its reads. */
        var asked: (() -> Unit)? = null
        override val online: MutableStateFlow<Boolean>
            get() {
                asked?.invoke()
                return state
            }
        var unreachable = 0

        override suspend fun probe() = state.value

        override fun answered() = Unit

        override fun unreachable() {
            unreachable++
        }
    }

    /** A copy of [version], its second page sized by its manifest only. */
    private suspend fun DownloadStore.download(version: String, at: Long) = runNext { run ->
        val manifest = OfflineManifest(
            1, run.contentId, version, fileSize = 10, fileMtime = "2026-01-10T06:00:00Z", pageCount = 2, from = 0,
            pages = listOf(OfflinePage("a.png"), OfflinePage("b.png", width = 800, height = 1200)),
        )
        File(run.dir, MANIFEST).writeText(AppJson.encodeToString(OfflineManifest.serializer(), manifest))
        manifest(run, manifest, at)
        for (i in 0..1) {
            File(run.dir, "$i").writeText("$version page $i")
            page(run, i, 8)
        }
        publish(run, CopyMade(run.dir.listFiles()!!.sumOf { it.length() }, cover = false, seriesCover = false))
    }!!.result!!

    private suspend fun failure(block: suspend () -> Unit) = try {
        block()
        null
    } catch (e: Exception) {
        e
    }

    @Test
    fun opensCopiesLocallyWhenTheyAreCurrentOrTheServerCant() = runBlocking {
        withTimeout(10_000) {
            val net = Net(FakeReaderData().apply { comics["c_v1"] = volume("c_v1"); comics["c_cut"] = volume("c_cut") })
            val conn = Conn()
            lateinit var store: DownloadStore
            var refuse = false
            store = DownloadStore(AccountStore("a", temp.root, db), scope, { store.collectSoon(it.dirId) }, committed = { if (refuse) throw StorageFullException() })
            val reader = OfflineFirstReaderData(net, MutableStateFlow(store), MutableStateFlow(null), conn, FakeReadingSync(), { null }, clock = { 35 })
            fun pinned() = runBlocking { PinKey(temp.root.name, db.downloads().get("c_v1")!!.copyId!!) }.let(Pins::pinned)

            /** Opened, then let go: whether it was the copy. */
            suspend fun isLocal(id: String = "c_v1"): Boolean {
                val owner = CoroutineScope(Job())
                return try {
                    reader.open(id, owner).pages.page(0) is File
                } finally {
                    owner.cancel()
                }
            }

            // Not downloaded: the server's, for the downloads' account.
            assertFalse(isLocal())
            assertEquals(listOf<String?>("a"), net.accounts)

            db.cache(listOf(Content("c_v1", "Vol. 1", ContentType.COMIC) to null), 1) { true }
            store.enqueue(listOf(NewDownload("c_v1", "c_v1", RequestedBy.USER)))
            assertTrue(store.download("v1", at = 10))

            // Restored at process start, before the account's downloads are open: they are waited for, unless they can't be opened.
            val opening = MutableStateFlow<DownloadStore?>(null)
            val failed = MutableStateFlow<String?>(null)
            val early = OfflineFirstReaderData(net, opening, failed, conn, FakeReadingSync(), { "a" })
            val restored = CoroutineScope(Job())
            val waiting = async(start = CoroutineStart.UNDISPATCHED) { early.open("c_v1", restored).pages.page(0) is File }
            assertFalse(waiting.isCompleted)
            opening.value = store
            assertTrue(waiting.await())
            restored.cancel()
            opening.value = null
            failed.value = "a"
            assertFalse(early.open("c_v1", restored).pages.page(0) is File)

            // Downloaded: the copy's files, sized by its manifest.
            val owner = CoroutineScope(Job())
            val opened = reader.open("c_v1", owner)
            assertEquals(listOf(PageDimensions(0, 0), PageDimensions(800, 1200)), opened.pages.pages)
            val held = opened.pages.page(1) as File
            assertEquals("v1 page 1", opened.pages.read(1) { it.readText() })

            // A page cut short since it was stored: found when the copy is opened, and marked. Online it is the server's pages, offline the copy's.
            db.cache(listOf(Content("c_cut", "Vol. 2", ContentType.COMIC) to null), 1) { true }
            store.enqueue(listOf(NewDownload("c_cut", "c_cut", RequestedBy.USER)))
            assertTrue(store.download("v1", at = 11))
            assertTrue(isLocal("c_cut"))
            File(temp.root, "downloads/${db.downloads().get("c_cut")!!.copyId}/1").writeText("cut")
            assertFalse(isLocal("c_cut"))
            assertEquals(Stale.DAMAGED, db.downloads().get("c_cut")!!.stale)
            conn.online.value = false
            assertTrue(isLocal("c_cut"))
            conn.online.value = true
            // A page that won't decode marks its copy too; not an out-of-memory error, which says nothing about the files.
            val oom = OutOfMemoryError()
            assertSame(oom, opened.pages.failed(0, oom))
            assertNull(db.downloads().get("c_v1")!!.stale)
            assertTrue(opened.pages.failed(0, IllegalStateException("Failed to decode image")) is PageDamaged)
            withTimeout(2_000) { while (db.downloads().get("c_v1")!!.stale != Stale.DAMAGED) delay(10) }

            // Updated on the server, then cut short: the length check runs all the same, and offline it is the copy, marked.
            db.cache(listOf(Content("c_old", "Vol. 3", ContentType.COMIC) to null), 1) { true }
            store.enqueue(listOf(NewDownload("c_old", "c_old", RequestedBy.USER)))
            assertTrue(store.download("v1", at = 12))
            store.observe(listOf(Observation("c_old", ServerFile(true, "2026-02-10T06:00:00Z", 10), at = 21)))
            assertEquals(Stale.VERSION, db.downloads().get("c_old")!!.stale)
            File(temp.root, "downloads/${db.downloads().get("c_old")!!.copyId}/1").writeText("cut")
            conn.online.value = false
            assertTrue(isLocal("c_old"))
            assertEquals(Stale.DAMAGED, db.downloads().get("c_old")!!.stale)
            conn.online.value = true

            // Updated on the server: the server's copy online, with nothing pinned; the local one offline, or when the server can't be reached.
            store.observe(listOf(Observation("c_v1", ServerFile(true, "2026-02-10T06:00:00Z", 10), at = 20)))
            owner.cancel()
            assertFalse(isLocal())
            assertFalse(pinned())
            conn.online.value = false
            assertTrue(isLocal())
            conn.online.value = true
            net.failure = IOException("cut")
            assertTrue(isLocal())
            assertEquals(1, conn.unreachable)

            // Another account signed in meanwhile: that is said, and neither the copy nor a fallback is opened.
            net.failure = AccountChangedException()
            assertTrue(failure { isLocal() } is SyncUnavailable.AccountChanged)
            assertEquals(1, conn.unreachable)
            assertFalse(pinned())
            // Another failure of the server (a 500, say): the copy, and the server isn't taken for unreachable.
            net.failure = IllegalStateException("broken")
            assertTrue(isLocal())
            assertEquals(1, conn.unreachable)
            assertFalse(pinned())
            net.failure = null

            // Downloaded again after opening: the reader is told to reopen; the copy it opened stays while its owner or a hold lasts.
            conn.online.value = false
            val reading = CoroutineScope(Job())
            val local = reader.open("c_v1", reading)
            conn.online.value = true
            store.again("c_v1")
            assertTrue(store.download("v2", at = 30))
            local.replaced.first()
            val hold = local.pages.hold()!!
            reading.cancel()
            assertEquals("v1 page 1", held.readText())
            hold.close()
            store.detachedBytes.first { it == 0L }
            assertFalse(held.exists())
            assertNull(local.pages.hold())
            assertTrue(failure { local.pages.read(0) { it.readText() } } is FileNotFoundException)

            // Gone, as the server says to the open itself: marked as of that request, and the copy opens. From then on always the copy.
            store.observe(listOf(Observation("c_v1", ServerFile(true, "2026-03-10T06:00:00Z", 10), at = 32)))
            net.failure = HttpException(Response.error<Unit>(404, """{"error": "Content not found"}""".toResponseBody("application/json".toMediaType())))
            // A mark the store refuses (a full disk) doesn't keep the intact copy from opening.
            refuse = true
            assertTrue(isLocal())
            refuse = false
            assertTrue(isLocal())
            assertEquals(listOf<Any?>(Stale.GONE, 35L), db.downloads().get("c_v1")!!.let { listOf(it.stale, it.seenAt) })
            val asked = net.accounts.size
            assertTrue(isLocal())
            assertEquals(asked, net.accounts.size)
            net.failure = null

            // A manifest that can't be read: the server's copy.
            val copy = File(temp.root, "downloads/${db.downloads().get("c_v1")!!.copyId}")
            val manifest = File(copy, MANIFEST).readText()
            File(copy, MANIFEST).writeText("{")
            assertFalse(isLocal())
            assertFalse(pinned())
            File(copy, MANIFEST).writeText(manifest)

            // The account's downloads close between the pin and the reads: said so, with no request to the server.
            store.observe(listOf(Observation("c_v1", ServerFile(true, "2026-03-10T06:00:00Z", 10), at = 40)))
            val before = net.accounts.size
            conn.online.value = false
            conn.asked = {
                conn.asked = null
                runBlocking { store.stop() }
            }
            assertTrue(failure { isLocal() } is SyncUnavailable.AccountChanged)
            assertEquals(before, net.accounts.size)
            assertFalse(pinned())
        }
    }
}
