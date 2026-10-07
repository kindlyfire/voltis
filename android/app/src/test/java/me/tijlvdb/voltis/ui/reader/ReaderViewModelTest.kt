package me.tijlvdb.voltis.ui.reader

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.ViewModelStore
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import androidx.lifecycle.viewModelScope
import java.io.File
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.domain.reading.LaneView
import me.tijlvdb.voltis.domain.reading.SeriesInfo
import me.tijlvdb.voltis.domain.reading.SyncUnavailable
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.kit.VTone
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.auth.testStore
import me.tijlvdb.voltis.data.reading.EngineTest
import me.tijlvdb.voltis.data.settings.ReaderSettingsStore
import me.tijlvdb.voltis.domain.comic.FakeReaderData
import me.tijlvdb.voltis.domain.comic.PageDimensions
import me.tijlvdb.voltis.domain.comic.ReaderMode
import me.tijlvdb.voltis.domain.comic.Siblings
import me.tijlvdb.voltis.domain.comic.volume
import me.tijlvdb.voltis.domain.reading.FakeReadingSync
import me.tijlvdb.voltis.domain.reading.PositionLabel
import me.tijlvdb.voltis.domain.reading.ReadingFailure
import me.tijlvdb.voltis.domain.reading.position
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

/** The reader cases of `ComicDisplay/useComicDisplayStore.test.ts`, on a fake [FakeReaderData]. */
@OptIn(ExperimentalCoroutinesApi::class)
class ReaderViewModelTest {
    @get:Rule
    val tmp = TemporaryFolder()

    private val data = FakeReaderData().apply {
        lists["s_1"] = listOf("c_1", "c_2", "c_3")
        for (id in lists.getValue("s_1")) comics[id] = volume(id, "s_1")
    }

    @Before
    fun setUp() = Dispatchers.setMain(UnconfinedTestDispatcher())

    @After
    fun tearDown() {
        readers.forEach { it.viewModelScope.cancel() }
        stores.cancel()
        Dispatchers.resetMain()
    }

    // Its scheduler is never run: the session never loads, so nothing hops to a real IO thread that
    // could outlive the test, and without a server the settings are the defaults, and no file is read.
    private val stores = CoroutineScope(SupervisorJob() + StandardTestDispatcher())
    private val readers = mutableListOf<ReaderViewModel>()

    private val settings by lazy {
        ReaderSettingsStore(
            PreferenceDataStoreFactory.create(scope = stores) { File(tmp.root, "device.preferences_pb") },
            testStore(File(tmp.root, "session.preferences_pb"), stores),
        )
    }

    private val sync = FakeReadingSync()
    private val connectivity = EngineTest.FakeConnectivity { true }

    private fun reader(id: String) = ReaderViewModel(id, data, settings, sync, connectivity).also { readers += it }

    private val ReaderViewModel.calls get() = (this.sync as FakeReadingSync.FakeSession).calls

    private fun savedAt(page: Int) = UserData(progress = JsonObject(mapOf("current_page" to JsonPrimitive(page))))

    @Test
    fun siblingsOpenAtTheirSavedPosition() {
        data.comics["c_2"] = volume("c_2", "s_1").copy(userData = savedAt(2))
        val first = reader("c_1")
        assertEquals(0, first.page)
        assertNull(first.siblings.prev)
        assertEquals("c_2", first.siblings.next?.id)

        // The screen replaces the reader entry: a new view model for the sibling.
        val second = reader("c_2")
        assertEquals(2, second.page)
        assertEquals("c_1", second.siblings.prev?.id)

        // The connection drops after the siblings loaded (a probe failing later): the next volume
        // that isn't downloaded is still named, but can't be gone to, also when chosen just before.
        data.offline.value = setOf("c_3")
        assertEquals("c_3", second.siblings.next?.id)
        assertNull(second.readableNext)
        assertEquals("c_1", second.readablePrev?.id)
        val opened = mutableListOf<String>()
        second.openVolume("c_3", opened::add)
        second.openVolume("c_1", opened::add)
        assertEquals(listOf("c_1"), opened)
        // Back online.
        data.offline.value = emptySet()
        assertEquals("c_3", second.readableNext?.id)

        // Gone from the server: a download still opens, where its copy says it was read to.
        sync.loadFailure = ReadingFailure.Gone("Content not found")
        val gone = reader("c_2")
        assertNull(gone.error)
        assertEquals(2, gone.page)
    }

    @Test
    fun pastTheEndWaitsForTheSiblings() {
        for (fail in listOf(false, true)) {
            val gate = CompletableDeferred<Unit>()
            data.listGate = gate.takeIf { !fail }
            data.failLists = if (fail) 1 else 0
            val vm = reader("c_2")
            vm.onPageSettled(2)
            vm.onReachedEnd()
            assertTrue(vm.atEnd)
            assertEquals(if (fail) Siblings.Status.Error else Siblings.Status.Loading, vm.siblings.status)
            // Nothing to go on to yet: the end card shows the spinner or the retry.
            assertNull(vm.siblings.next)

            if (fail) vm.retrySiblings() else gate.complete(Unit)
            assertEquals("c_3", vm.siblings.next?.id)
            assertTrue(vm.atEnd)
        }
    }

    @Test
    fun inputIsIgnoredUntilTheSavedPageIsPlaced() {
        data.comics["c_1"] = volume("c_1", "s_1").copy(userData = savedAt(1))
        val gate = CompletableDeferred<Unit>()
        data.openGate = gate
        val vm = reader("c_1")
        vm.onPageSettled(2)
        vm.placePage(2)
        vm.onReachedEnd()
        assertNull(vm.comic)
        assertEquals(0, vm.page)
        assertFalse(vm.atEnd)

        gate.complete(Unit)
        assertNotNull(vm.comic)
        assertEquals(1, vm.page)
    }

    @Test
    fun anUnchangedPageDoesNothing() {
        data.pageCount = 30
        val vm = reader("c_1")
        // The first ten pages in preload order.
        assertEquals((0..9).toList(), data.preloaded)

        vm.onPageSettled(0)
        assertEquals(10, data.preloaded.size)
        vm.onPageSettled(4)
        assertEquals(4, vm.page)
        // Pages 10 and 11 are new among the ten around page 4.
        assertEquals((0..11).toList(), data.preloaded)
        vm.onPageSettled(4)
        vm.placePage(4)
        assertEquals(12, data.preloaded.size)
        assertEquals(0, vm.placement)
        // Past the end is clamped.
        vm.placePage(99)
        assertEquals(29, vm.page)
        assertEquals(1, vm.placement)
        // Leaving the end card for the page it is on is a placement too.
        vm.onReachedEnd()
        vm.placePage(29)
        assertFalse(vm.atEnd)
        assertEquals(2, vm.placement)
    }

    @Test
    fun readingIsReportedToTheSync() {
        data.pageCount = 30
        sync.saved["c_1"] = position(12, 30)
        val store = ViewModelStore()
        val vm = ViewModelProvider.create(store, viewModelFactory { initializer { reader("c_1") } })[ReaderViewModel::class]
        // The sync decides the opening page, which is placed.
        assertEquals(12, vm.page)
        assertEquals(listOf("load", "placed:12"), vm.calls)

        vm.onPageSettled(13)
        vm.onPageSettled(13)
        vm.placePage(20)
        vm.onReachedEnd()
        // Another device's position, or an Undo.
        (vm.sync as FakeReadingSync.FakeSession).adapter.restore(position(4, 30))
        assertEquals(4, vm.page)
        // A rotation reports neither pause nor resume, so a repeat changes nothing.
        vm.setVisible(true)
        vm.setVisible(true)
        store.clear()
        assertEquals(
            listOf("load", "placed:12", "moved:13", "placed:20", "finish:29", "placed:4", "visible:true", "detach"),
            vm.calls,
        )
    }

    @Test
    fun theOpeningPageWaitsForTheSync() {
        sync.saved["c_1"] = position(2, 3)
        sync.loadGate = CompletableDeferred()
        sync.failLoads = 1
        val vm = reader("c_1")
        val adapter = (vm.sync as FakeReadingSync.FakeSession).adapter
        // Opened but not placed: positions compare by its pages, and nothing places it yet.
        assertEquals(PositionLabel.Page(3), adapter.describe(position(9, 3)))
        adapter.restore(position(2, 3))
        assertNull(vm.comic)

        sync.loadGate!!.complete(Unit)
        assertNotNull(vm.error)
        vm.load()
        // Retry loads the one session again. It opens where the sync says, also after process
        // death: the lane's unsent page if it has one, else the server's, which may be newer.
        assertEquals(1, sync.sessions.size)
        assertEquals(2, vm.page)
        assertEquals(listOf("load", "load", "placed:2"), vm.calls)
    }

    @Test
    fun onlyTheLatestLoadPublishes() {
        // A double Retry: the newer open answers first, and the older one, still waiting, never shows.
        val older = CompletableDeferred<Unit>()
        data.openGate = older
        val vm = reader("c_1")
        val newer = CompletableDeferred<Unit>()
        data.openGate = newer
        vm.load()
        data.pageCount = 5
        newer.complete(Unit)
        assertEquals(5, vm.comic?.pages?.pages?.size)
        data.pageCount = 3
        data.failOpens = 1
        older.complete(Unit)
        assertEquals(5, vm.comic?.pages?.pages?.size)
        assertNull(vm.error)
        // Only the shown copy is kept.
        assertEquals(listOf(false, true), data.owners.map { it.isActive })
    }

    @Test
    fun aSeriesOrABookRedirects() {
        for (type in listOf(ContentType.COMIC_SERIES, "book")) {
            data.comics["x_1"] = volume("x_1", type = type)
            val vm = reader("x_1")
            assertEquals("x_1", vm.redirect)
            assertNull(vm.comic)
            assertNull(vm.error)
        }
    }

    @Test
    fun aReplacedCopyIsOnePlacement() {
        val vm = reader("c_1")
        val session = vm.sync as FakeReadingSync.FakeSession
        vm.placePage(2)
        vm.onReachedEnd()
        val sessions = sync.sessions.size
        val placements = vm.placement
        var seen = vm.calls.size
        fun newCalls() = vm.calls.drop(seen).also { seen = vm.calls.size }

        // More pages. While the new copy is prepared the old one shows and is kept: restores are dropped, the user's own moves apply.
        data.pageCount = 5
        sync.pageCountGate = CompletableDeferred()
        data.replaced.tryEmit(Unit)
        assertEquals(listOf("invalidateRestores"), newCalls())
        session.adapter.restore(position(0, 3))
        assertEquals(listOf<Any>(3, 2, true, placements), listOf(vm.comic!!.pages.pages.size, vm.page, vm.atEnd, vm.placement))
        vm.onPageSettled(1)
        assertEquals(listOf("moved:1"), newCalls())
        assertEquals(2, data.kept)

        // Then one publication: on the same page, off the end card (which the move left already), placed once in the same session.
        sync.pageCountGate!!.complete(Unit)
        sync.pageCountGate = null
        assertEquals(listOf(5, 1), listOf(vm.comic!!.pages.pages.size, vm.page))
        assertFalse(vm.atEnd)
        assertEquals(listOf("placed:1"), newCalls())
        assertEquals(listOf("c_1" to 5), sync.pageCounts)
        assertEquals(sessions, sync.sessions.size)
        // The old copy's owner ended once the new one showed.
        assertEquals(listOf(false, true), data.owners.map { it.isActive })
        session.adapter.restore(position(3, 5))
        assertEquals(listOf("placed:3"), newCalls())

        // Fewer pages while at the end: clamped to the new last page, still at the end; no reading or finish of its own.
        vm.placePage(4)
        vm.onReachedEnd()
        newCalls()
        data.pageCount = 2
        data.replaced.tryEmit(Unit)
        assertEquals(1, vm.page)
        assertTrue(vm.atEnd)
        assertEquals(listOf("invalidateRestores", "placed:1"), newCalls())
        assertEquals(1, data.kept)

        // In longstrip the same: the end of the strip, then more pages.
        data.pageSize = PageDimensions(800, 12000)
        data.pageCount = 3
        val strip = reader("c_2")
        assertEquals(ReaderMode.Longstrip, strip.mode)
        strip.placePage(2)
        strip.onReachedEnd()
        val before = strip.calls.size
        data.pageCount = 4
        data.replaced.tryEmit(Unit)
        assertEquals(listOf(4, 2), listOf(strip.comic!!.pages.pages.size, strip.page))
        assertFalse(strip.atEnd)
        assertEquals(listOf("invalidateRestores", "placed:2"), strip.calls.drop(before))
    }

    @Test
    fun whatIsntShownIsLetGo() {
        // A reopen whose page count can't be stored: the new copy isn't shown, nothing is placed, and both copies are let go.
        val vm = reader("c_1")
        vm.onReachedEnd()
        val placed = vm.calls.size
        sync.pageCountFailure = IllegalStateException("stopped")
        data.pageCount = 5
        data.replaced.tryEmit(Unit)
        assertNull(vm.comic)
        assertNotNull(vm.error)
        assertEquals(listOf("invalidateRestores"), vm.calls.drop(placed))
        assertEquals(0, data.kept)
        // Retry loads it anew, where the sync says: not on the end card the old copy was left on.
        sync.pageCountFailure = null
        sync.saved["c_1"] = position(1, 5)
        vm.load()
        assertEquals(5, vm.comic?.pages?.pages?.size)
        assertEquals(1, vm.page)
        assertFalse(vm.atEnd)
        assertEquals(1, data.kept)
        // The failed reopen is over: the sync places the reader again.
        (vm.sync as FakeReadingSync.FakeSession).adapter.restore(position(3, 5))
        assertEquals(3, vm.page)

        // An open that fails, a failed sync load, a redirect: nothing kept. Retry keeps one, until the view model is cleared.
        data.owners.clear()
        data.failOpens = 1
        sync.failLoads = 1
        val store = ViewModelStore()
        val failing = ViewModelProvider.create(store, viewModelFactory { initializer { reader("c_2") } })[ReaderViewModel::class]
        assertNotNull(failing.error)
        failing.load()
        assertNotNull(failing.error)
        assertEquals(0, data.kept)
        data.comics["x_1"] = volume("x_1", type = ContentType.COMIC_SERIES)
        reader("x_1")
        assertEquals(0, data.kept)
        failing.load()
        assertNotNull(failing.comic)
        assertEquals(1, data.kept)
        store.clear()
        assertEquals(0, data.kept)
    }

    @Test
    fun theStatusRowAndTheEndCardActThroughTheSync() {
        // `ReaderStatusRow.test.ts`: the chip, the primary action and the rest, on the shown status.
        val held = SeriesInfo("s_1", status = "on_hold")
        val caughtUp = SeriesInfo("s_1", status = "reading", caughtUp = true)
        val cases = listOf(
            LaneView(status = "plan_to_read") to StatusRowModel(StatusChip.STATUS, VTone.Neutral, StatusAction.COMPLETE, listOf(StatusAction.READING, StatusAction.RESET)),
            LaneView(status = "completed", series = held) to
                StatusRowModel(StatusChip.STATUS, VTone.Success, StatusAction.RESET, listOf(StatusAction.RESUME_SERIES, StatusAction.READING)),
            LaneView(status = "reading", series = held) to
                StatusRowModel(StatusChip.SERIES_HELD, VTone.Warning, StatusAction.RESUME_SERIES, listOf(StatusAction.COMPLETE, StatusAction.RESET)),
            LaneView(status = "reading", series = caughtUp) to StatusRowModel(StatusChip.CAUGHT_UP, VTone.Primary, StatusAction.COMPLETE, listOf(StatusAction.RESET)),
            LaneView(status = "on_hold", tracking = false) to
                StatusRowModel(StatusChip.NOT_TRACKING, VTone.Neutral, StatusAction.TRACK, listOf(StatusAction.COMPLETE, StatusAction.READING, StatusAction.RESET)),
        )
        for ((view, row) in cases) assertEquals(row, statusRow(view))

        // The last volume asks for an earlier unread one; the others don't.
        data.earlier["s_1"] = "c_1"
        assertNull(reader("c_2").earlierUnread)
        val vm = reader("c_3")
        assertEquals("c_1", vm.earlierUnread)
        // Asked again when the series' reading changes: completed with its unread volumes.
        data.earlier.clear()
        (vm.sync as FakeReadingSync.FakeSession).view.value = LaneView(series = SeriesInfo("s_1", status = "completed"))
        assertNull(vm.earlierUnread)
        // And when the server is back.
        connectivity.online.value = false
        data.earlier["s_1"] = "c_1"
        connectivity.online.value = true
        assertEquals("c_1", vm.earlierUnread)

        for (action in StatusAction.entries) vm.runStatus(action)
        assertEquals(
            listOf("trackProgress", "resetAndReadAgain", "seriesCommand:reading", "command:mark_completed", "command:set_status:reading"),
            vm.calls.drop(2),
        )
        // A failure is a snackbar.
        val messages = mutableListOf<UiText>()
        vm.viewModelScope.launch { vm.messages.collect { messages += it } }
        sync.commandFailure = SyncUnavailable.NeedsConnection()
        vm.runStatus(StatusAction.COMPLETE)
        assertFalse(vm.statusBusy)
        assertEquals(listOf(UiText.Format(R.string.reader_update_failed, listOf(UiText.Res(R.string.error_needs_connection)))), messages)
    }
}
