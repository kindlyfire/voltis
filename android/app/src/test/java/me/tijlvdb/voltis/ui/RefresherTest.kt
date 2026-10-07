package me.tijlvdb.voltis.ui

import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.ui.grid.GridSource
import me.tijlvdb.voltis.ui.grid.follows
import org.junit.Assert.assertEquals
import org.junit.Test

class RefresherTest {
    @OptIn(ExperimentalCoroutinesApi::class)
    @Test
    fun refreshesWhenDirtyOrStale() = runTest(UnconfinedTestDispatcher()) {
        val events = CatalogEvents()
        var now = 0L
        var refreshes = 0
        val refresher = Refresher(backgroundScope, events, { it is CatalogChange.UserDataChanged }, { now }) { refreshes++ }
        val match = CatalogChange.UserDataChanged("c_1")

        // The first resume has nothing to catch up on; the screen loads itself.
        refresher.onResume()
        assertEquals(0, refreshes)
        // Resumed: a matching change refreshes at once, another kind doesn't.
        events.emit(match)
        events.emit(CatalogChange.PreferencesChanged)
        assertEquals(1, refreshes)

        // Under the reader or in another tab: changes wait for the resume, and refresh once.
        refresher.onPause()
        events.emit(match)
        events.emit(match)
        assertEquals(1, refreshes)
        refresher.onResume()
        assertEquals(2, refreshes)

        // A short absence without changes refreshes nothing; a long one does.
        refresher.onPause()
        now += 30_000
        refresher.onResume()
        assertEquals(2, refreshes)
        refresher.onPause()
        now += 30_001
        refresher.onResume()
        assertEquals(3, refreshes)
    }

    /** A burst of changes: one refresh at once, one at the window's end, none while paused. */
    @OptIn(ExperimentalCoroutinesApi::class)
    @Test
    fun throttlesBursts() = runTest(UnconfinedTestDispatcher()) {
        val events = CatalogEvents()
        var refreshes = 0
        val refresher = Refresher(backgroundScope, events, { true }, { 0L }) { refreshes++ }
        val change = CatalogChange.UserDataChanged("c_1")
        refresher.onResume()

        repeat(5) { events.emit(change) }
        assertEquals(1, refreshes)
        advanceTimeBy(499)
        assertEquals(1, refreshes)
        advanceTimeBy(2)
        assertEquals(2, refreshes)
        // That refresh opened a window too, with nothing in it.
        advanceTimeBy(1_000)
        assertEquals(2, refreshes)

        // Paused inside a window: its pending refresh waits for the resume.
        events.emit(change)
        events.emit(change)
        refresher.onPause()
        advanceTimeBy(1_000)
        assertEquals(3, refreshes)
        refresher.onResume()
        runCurrent()
        assertEquals(4, refreshes)
    }

    /** A batch costs each resumed screen one refresh, however its changes and end are delivered. Times are the refreshes'. */
    @OptIn(ExperimentalCoroutinesApi::class)
    @Test
    fun batchRefreshesOnce() {
        class Ctx(val scope: TestScope, val events: CatalogEvents, val refresher: Refresher, val refreshes: MutableList<Pair<String, Long>>) {
            fun another() = Refresher(scope.backgroundScope, events, ::follows, { 0L }) { refreshes += "late" to scope.testScheduler.currentTime }
                .also {
                    scope.runCurrent()
                    it.onResume()
                }
        }

        val reading = { id: String -> CatalogChange.ReadingChanged(id, id) }
        val ordinary = CatalogChange.UserDataChanged("c_series")
        fun main(vararg at: Long) = at.map { "main" to it }
        val cases = listOf<Triple<String, suspend Ctx.() -> Unit, List<Pair<String, Long>>>>(
            // 100 stamped changes and the end, all queued before the collector runs; then one more a second later.
            Triple("slow collector", {
                events.begin(1)
                repeat(100) { events.emit(reading("c_$it"), origin = 1) }
                events.end(1, emptyList())
                scope.advanceTimeBy(1_000)
                events.emit(reading("c_1"))
            }, main(0, 1_000)),
            Triple("no events, touched matches", {
                events.begin(1)
                events.end(1, listOf(reading("c_1")))
            }, main(0)),
            Triple("no events, touched doesn't match", {
                events.begin(1)
                events.end(1, listOf(CatalogChange.PositionSaved("c_1", "c_1")))
            }, main()),
            Triple("window already active", {
                events.emit(ordinary)
                scope.advanceTimeBy(100)
                events.begin(1)
                events.end(1, listOf(reading("c_1")))
            }, main(0, 500)),
            Triple("paused", {
                refresher.onPause()
                events.begin(1)
                events.emit(reading("c_1"), origin = 1)
                events.end(1, emptyList())
                scope.runCurrent()
                assertEquals(emptyList<Pair<String, Long>>(), refreshes)
                refresher.onResume()
            }, main(0)),
            Triple("subscribed mid-batch", {
                events.begin(1)
                events.emit(reading("c_1"), origin = 1)
                scope.runCurrent()
                another()
                events.emit(reading("c_2"), origin = 1)
                events.end(1, emptyList())
            }, main(0) + ("late" to 0L)),
            // A star on a selected series and a reader on an unselected sibling are ordinary: they keep their throttle.
            Triple("ordinary changes during a batch", {
                events.begin(1)
                events.emit(ordinary)
                scope.advanceTimeBy(600)
                events.emit(CatalogChange.ReadingChanged("c_w", "c_series"))
                scope.advanceTimeBy(100)
                events.end(1, listOf(CatalogChange.ReadingChanged("c_v", "c_series")))
            }, main(0, 600, 1_100)),
            Triple("stale origin", {
                events.begin(1)
                events.end(1, emptyList())
                events.emit(reading("c_1"), origin = 1)
            }, main(0)),
        )
        for ((name, script, expected) in cases) {
            val refreshes = mutableListOf<Pair<String, Long>>()
            runTest {
                val events = CatalogEvents()
                val refresher = Refresher(backgroundScope, events, ::follows, { 0L }) { refreshes += "main" to testScheduler.currentTime }
                val ctx = Ctx(this, events, refresher, refreshes)
                refresher.onResume()
                // The flow has no replay: subscribe before anything is emitted.
                runCurrent()
                ctx.script()
                runCurrent()
                advanceTimeBy(1_000)
                runCurrent()
            }
            assertEquals(name, expected, refreshes)
        }
    }

    private fun follows(change: CatalogChange) = change is CatalogChange.ReadingChanged || change is CatalogChange.UserDataChanged

    /** What a grid follows. */
    @Test
    fun gridsFollowChanges() {
        val contents = GridSource.Contents("c_series")
        val cases = listOf(
            Triple(contents, CatalogChange.ReadingChanged("c_vol", "c_series"), true),
            Triple(contents, CatalogChange.ReadingChanged("c_series", "c_series"), true),
            Triple(contents, CatalogChange.ReadingChanged("c_other", "c_elsewhere"), false),
            // A volume its filter left out, starred on its own page: the event names no series, and the list can gain it.
            Triple(contents, CatalogChange.UserDataChanged("c_excluded"), true),
            Triple(contents, CatalogChange.PreferencesChanged, false),
            Triple(GridSource.Library("l_1"), CatalogChange.ReadingChanged("c_other", "c_elsewhere"), true),
            // Every source follows ReadingChanged(item, parentOrSelf) of its own items: what lets a batch's end reload the grid that ran it.
            Triple(GridSource.Library("l_1"), CatalogChange.ReadingChanged("c_vol", "c_series"), true),
            Triple(GridSource.Browse(), CatalogChange.ReadingChanged("c_series", "c_series"), true),
            Triple(GridSource.Facet("tag", "x"), CatalogChange.ReadingChanged("c_vol", "c_series"), true),
            Triple(GridSource.Browse(), CatalogChange.UserDataChanged("c_other"), true),
            // A saved position refreshes Home only.
            Triple(GridSource.Library("l_1"), CatalogChange.PositionSaved("c_vol", "c_series"), false),
        )
        for ((source, change, expected) in cases) assertEquals("$source $change", expected, source.follows(change))
    }
}
