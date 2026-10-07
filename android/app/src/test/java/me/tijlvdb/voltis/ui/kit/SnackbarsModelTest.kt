package me.tijlvdb.voltis.ui.kit

import androidx.compose.runtime.snapshots.Snapshot
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class SnackbarsModelTest {
    /** Runs what is due, and lets the model see the host's change, as a frame does. */
    private fun TestScope.settle() {
        runCurrent()
        Snapshot.sendApplyNotifications()
        runCurrent()
    }

    private fun TestScope.model() = SnackbarsModel({ error("unused") }, backgroundScope, { testScheduler.currentTime })

    private fun SnackbarsModel.shown() = state.currentSnackbarData?.visuals?.message

    /** A newer plain message replaces the one showing after a second and drops the ones waiting; one that has just come on screen is never cut short. */
    @Test
    fun plainMessagesReplaceEachOther() = runTest {
        val model = model()
        model.show("a")
        settle()
        assertEquals("a", model.shown())

        // Visible for less than a second: the newest waits, the one before it never shows.
        advanceTimeBy(400)
        model.show("b")
        model.show("c")
        advanceTimeBy(500)
        settle()
        assertEquals("a", model.shown())
        advanceTimeBy(150)
        settle()
        assertEquals("c", model.shown())

        // Behind an action snackbar that goes, then a newer one at once: the first gets its second.
        backgroundScope.launch { model.state.showSnackbar("Marked read", "Undo") }
        runCurrent()
        model.show("d")
        advanceTimeBy(1_500)
        settle()
        // "c" had its second: the action snackbar queued first goes in, and "d" waits behind it.
        assertEquals("Marked read", model.shown())
        model.state.currentSnackbarData?.dismiss()
        runCurrent()
        assertEquals("d", model.shown())
        model.show("e")
        advanceTimeBy(900)
        settle()
        assertEquals("d", model.shown())
        advanceTimeBy(200)
        settle()
        assertEquals("e", model.shown())
    }

    /** Action snackbars keep their order: none replaces or cuts short another, and a plain message waits for them. */
    @Test
    fun actionSnackbarsQueue() = runTest {
        val model = model()
        val snackbars = Snackbars(model, backgroundScope)
        var undone = 0
        snackbars.show("Marked read", "Undo") { undone++ }
        runCurrent()
        snackbars.show("Couldn't clear", "Retry")
        model.show("plain")
        advanceTimeBy(60_000)
        settle()
        assertEquals("Marked read", model.shown())
        model.state.currentSnackbarData?.performAction()
        runCurrent()
        assertEquals(1, undone)
        assertEquals("Couldn't clear", model.shown())
        advanceTimeBy(60_000)
        settle()
        assertEquals("Couldn't clear", model.shown())
        model.state.currentSnackbarData?.dismiss()
        runCurrent()
        assertEquals("plain", model.shown())
    }

    /** Action snackbars asked for in turn show in turn, whichever scope owns them and however it dispatches. */
    @Test
    fun actionsKeepArrivalOrderAcrossScopes() = runTest {
        // The model's scope runs at once, as on the main thread; the composition's dispatches.
        val model = SnackbarsModel({ error("unused") }, CoroutineScope(backgroundScope.coroutineContext + UnconfinedTestDispatcher(testScheduler)), { testScheduler.currentTime })
        val snackbars = Snackbars(model, backgroundScope)
        backgroundScope.launch { model.state.showSnackbar("Showing", "Undo") }
        runCurrent()
        snackbars.show("View", "View")
        snackbars.show("Retry", "Retry", retained = true)
        model.state.currentSnackbarData?.dismiss()
        runCurrent()
        assertEquals("View", model.shown())
        model.state.currentSnackbarData?.dismiss()
        runCurrent()
        assertEquals("Retry", model.shown())
    }
}
