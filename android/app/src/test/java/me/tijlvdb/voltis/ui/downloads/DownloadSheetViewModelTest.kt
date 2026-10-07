package me.tijlvdb.voltis.ui.downloads

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.db.DownloadEntity
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Before
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class DownloadSheetViewModelTest {
    @Before
    fun setUp() = Dispatchers.setMain(UnconfinedTestDispatcher())

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun volume(id: String, status: String? = null) = Content(id, id, ContentType.COMIC, parentId = "s", userData = status?.let { UserData(status = it) })

    @Test
    fun theRangeStartsAtTheContinueTargetAndSurvivesReopeningAndProcessDeath() {
        var list = listOf(volume("v1", ReadingStatus.COMPLETED), volume("v2"), volume("v3"), volume("v4"))
        val rows = MutableStateFlow(emptyList<DownloadEntity>())
        val queued = mutableListOf<List<String>>()
        val saved = SavedStateHandle()
        fun vm() = DownloadSheetViewModel({ list }, { rows }, { items -> queued += items.map { it.id } }, continueOf = { "v3" }, saved = saved)
        var vm = vm()
        vm.load("s", keep = false)
        assertEquals(RangeSel(2, 3), vm.range)

        // A range the user made stays when the sheet reads again, by volume.
        vm.pickFrom(1)
        list = listOf(volume("v0")) + list
        vm.load("s", keep = true)
        assertEquals(RangeSel(2, 4), vm.range)

        // One volume, the last, is a range too; a new view model on the same saved state restores it.
        vm.pickFrom(4)
        assertEquals(RangeSel(4, 4), vm.range)
        vm = vm()
        assertEquals(RangeSel(), vm.range)
        vm.load("s", keep = true)
        assertEquals(RangeSel(4, 4), vm.range)

        // A cleared range stays cleared, and an end that is gone falls back to the defaults.
        vm.clear()
        vm = vm()
        vm.load("s", keep = true)
        assertEquals(RangeSel(), vm.range)
        vm.pickFrom(1)
        list = list.filter { it.id != "v1" }
        vm = vm()
        vm.load("s", keep = true)
        assertEquals(RangeSel(2, 3), vm.range)

        vm.download(rangePlan(vm.volumes!!, vm.range, mapOf("v4" to "done")).items) {}
        assertEquals(listOf(listOf("v3")), queued)
        // After a download, and on any fresh opening, the range starts from the defaults.
        vm.load("s", keep = true)
        assertEquals(RangeSel(2, 3), vm.range)
        vm.pickFrom(3)
        vm.load("s", keep = false)
        assertEquals(RangeSel(2, 3), vm.range)

        // Only the shortcut last tapped is marked, until the range is edited by hand.
        vm.shortcut(Shortcut.All)
        assertEquals(Shortcut.All, vm.chosen)
        vm.pickTo(2)
        assertEquals(null, vm.chosen)
    }

    @Test
    fun aFreshReopenIsNotEditableUntilItLoadsAndRetryKeepsItsPolicy() {
        val gate = CompletableDeferred<Unit>()
        var fail = false
        val saved = SavedStateHandle()
        val vm = DownloadSheetViewModel(
            { gate.await(); if (fail) error("offline"); listOf(volume("v1"), volume("v2"), volume("v3")) },
            { MutableStateFlow(emptyList()) }, {}, continueOf = { "v2" }, saved = saved,
        )
        vm.load("s", keep = false)
        gate.complete(Unit)
        vm.pickFrom(0)
        assertEquals(RangeSel(0, 2), vm.range)

        // The reopen reads again: nothing can be edited meanwhile, and its answer is not overwritten.
        val slow = CompletableDeferred<Unit>()
        val reopen = DownloadSheetViewModel(
            { slow.await(); if (fail) error("offline"); listOf(volume("v1"), volume("v2"), volume("v3")) },
            { MutableStateFlow(emptyList()) }, {}, continueOf = { "v2" }, saved = saved,
        )
        reopen.load("s", keep = false)
        assertEquals(null, reopen.volumes)
        reopen.pickFrom(2)
        reopen.clear()
        slow.complete(Unit)
        assertEquals(RangeSel(1, 2), reopen.range)

        // A failed reload shows its error and no list; retry repeats keep = true and restores the range.
        reopen.pickFrom(2)
        fail = true
        reopen.load("s", keep = true)
        assertEquals(null, reopen.volumes)
        assertNotNull(reopen.error)
        fail = false
        reopen.retry("s")
        assertEquals(RangeSel(2, 2), reopen.range)
        assertEquals(null, reopen.error)
    }
}
