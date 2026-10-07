package me.tijlvdb.voltis.ui.lists

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.Snapshot
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.async
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.yield
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class HandoverTest {
    @Test
    fun removalTargets() {
        val order = listOf("a", "b", "c", "d")
        for ((gone, shown, expected) in listOf(
            // The next survivor.
            Triple(setOf("b"), listOf("a", "c", "d"), "c"),
            // The last row: the one before.
            Triple(setOf("d"), listOf("a", "b", "c"), "c"),
            // The next one went meanwhile: the one after it.
            Triple(setOf("b"), listOf("a", "d"), "d"),
            // The deleted entry was another row's (a retry): both are passed over.
            Triple(setOf("b", "c"), listOf("a", "d"), "d"),
        )) {
            assertEquals(expected, removalTarget(order, gone, shown))
        }
        // The only row: the title.
        assertEquals(null, removalTarget(listOf("a"), setOf("a"), emptyList()))
    }

    @Test
    fun positionalTargetsEnd() {
        val order = listOf("a", "b", "c")
        // The reload (revision 3) isn't displayed yet: wait, whatever rows are shown.
        assertEquals(Target.Wait, movedTarget("a", 3, 2, true, order))
        assertEquals(Target.Wait, removedTarget(setOf("b"), order, 3, 2, true, order))
        // It is, or a newer refresh is, which may have restored the very rows from before the move: never a wait.
        assertEquals(Target.Go("a"), movedTarget("a", 3, 3, true, listOf("b", "a", "c")))
        assertEquals(Target.Go("a"), movedTarget("a", 3, 4, true, order))
        assertEquals(Target.Skip, movedTarget("a", 3, 4, true, listOf("b", "c")))
        assertEquals(Target.Go("c"), removedTarget(setOf("b"), order, 3, 4, true, order))
        assertEquals(Target.Go("c"), removedTarget(setOf("b"), order, 3, 3, true, listOf("a", "c")))
        // The list was deleted: its revision is gone with it, so there is nothing to wait for.
        assertEquals(Target.Skip, removedTarget(setOf("b"), order, 3, 0, false, emptyList()))
        assertEquals(Target.Skip, movedTarget("a", 3, 0, false, emptyList()))
    }

    /** A page of rows a to d under the header; [visible] lazy indexes are fully visible, [partly] partly. */
    private class Page : HandoverPorts {
        var shown by mutableStateOf(listOf("a", "b", "c", "d"))
        var visible by mutableStateOf(setOf(0, 1, 2, 3, 4))
        var partly by mutableStateOf(emptySet<Int>())
        var anchors by mutableStateOf(setOf(null, "a", "b", "c", "d"))

        /** The rows as the layout still has them; null: as [shown]. */
        var laid by mutableStateOf<List<String>?>(null)
        val focused = mutableListOf<String?>()
        val scrolls = mutableListOf<Int>()

        /** A scroll suspends here before it changes anything. */
        var scrollGate: CompletableDeferred<Unit>? = null
        var afterScroll: (Int) -> Unit = {}

        override val displayed get() = shown

        override fun lazyIndex(target: String?) = if (target == null) 0 else 1 + shown.indexOf(target)

        private fun laidIndex(target: String?) = if (target == null) 0 else 1 + (laid ?: shown).indexOf(target)

        override fun lagging(target: String?) = laidIndex(target) != lazyIndex(target)

        override fun fullyVisible(target: String?) = laidIndex(target) in visible

        override fun partlyVisible(target: String?) = laidIndex(target).let { it in visible || it in partly }

        override fun anchored(target: String?) = target in anchors

        override fun requestFocus(target: String?) {
            focused += target
        }

        override suspend fun scrollTo(index: Int) {
            scrollGate?.await()
            scrolls += index
            afterScroll(index)
        }
    }

    private var attention by mutableIntStateOf(0)
    private var exploring by mutableStateOf(true)
    private val valid = { intent: FocusIntent? -> intent != null && attention == intent.attention && exploring }
    private val intent = FocusIntent(0)

    /** A change the handover's waits see, then time for them to run. */
    private suspend fun change(block: () -> Unit) {
        block()
        Snapshot.sendApplyNotifications()
        repeat(10) { yield() }
    }

    private fun CoroutineScope.start(page: Page, target: (List<String>) -> Target) = async { handOver(intent, valid, page, target) }

    private fun removal(gone: String) = { shown: List<String> ->
        if (gone in shown) Target.Wait else Target.Go(removalTarget(listOf("a", "b", "c", "d"), setOf(gone), shown))
    }

    @Test
    fun aDeletedListEndsTheHandover() = runBlocking {
        // Deleted before the handover: it settles at once, without focus. Deleted while it waits for the reload's revision: the same.
        for (waiting in listOf(false, true)) {
            val page = Page()
            var present by mutableStateOf(!waiting.not())
            var displayed by mutableIntStateOf(0)
            if (!waiting) present = false
            val run = start(page) { shown -> removedTarget(setOf("b"), listOf("a", "b", "c", "d"), 1, displayed.toLong(), present, shown) }
            change { }
            if (waiting) {
                assertFalse(run.isCompleted)
                change {
                    present = false
                    displayed = 0
                    page.shown = emptyList()
                }
            }
            assertFalse(withTimeout(1_000) { run.await() })
            assertEquals(emptyList<String?>(), page.focused)
        }
    }

    @Test
    fun handsOverOnlyWhileValid() = runBlocking {
        // Already invalid: nothing scrolls, nothing is focused. The user acted, or TalkBack went off.
        for (lapse in listOf<() -> Unit>({ attention++ }, { exploring = false })) {
            val page = Page().apply { visible = setOf(0) }
            change(lapse)
            assertFalse(handOver(intent, valid, page) { Target.Go("c") })
            assertEquals(emptyList<Int>(), page.scrolls)
            assertEquals(emptyList<String?>(), page.focused)
            change {
                attention = 0
                exploring = true
            }
        }

        // The happy path waits for the removed row to leave, then focuses the next one once.
        var page = Page()
        var run = start(page, removal("b"))
        change { }
        assertEquals(emptyList<String?>(), page.focused)
        change { page.shown = listOf("a", "c", "d") }
        assertTrue(withTimeout(1_000) { run.await() })
        assertEquals(listOf<String?>("c"), page.focused)

        // Only the anchor's placement changes: that is seen, and focus is requested once.
        page = Page().apply {
            shown = listOf("a", "c", "d")
            anchors = setOf(null, "a", "d")
        }
        run = start(page, removal("b"))
        change { }
        assertEquals(emptyList<String?>(), page.focused)
        change { page.anchors += "c" }
        assertTrue(withTimeout(1_000) { run.await() })
        assertEquals(listOf<String?>("c"), page.focused)

        // Only visibility changes after the one scroll (a row taller than the screen is ready once part of it shows).
        page = Page().apply {
            shown = listOf("a", "c", "d")
            visible = setOf(0, 1)
        }
        run = start(page, removal("b"))
        change { }
        assertEquals(listOf(2), page.scrolls)
        change { page.partly = setOf(2) }
        assertTrue(withTimeout(1_000) { run.await() })
        assertEquals(listOf<String?>("c"), page.focused)
        assertEquals(listOf(2), page.scrolls)

        // An oversized header: the title is ready once part of it shows.
        page = Page().apply {
            shown = emptyList()
            visible = emptySet()
            afterScroll = { partly = setOf(it) }
        }
        assertTrue(withTimeout(1_000) { handOver(intent, valid, page) { Target.Go(null) } })
        assertEquals(listOf<String?>(null), page.focused)

        // The user acts while it waits for the anchor: no focus.
        page = Page().apply { anchors = setOf(null) }
        run = start(page) { Target.Go("c") }
        change { attention++ }
        change { page.anchors += "c" }
        assertFalse(withTimeout(1_000) { run.await() })
        assertEquals(emptyList<String?>(), page.focused)
    }

    @Test
    fun retargetsAndCancelsScrolls() = runBlocking {
        // The target goes while it is scrolled to: the next survivor is focused instead.
        var page = Page().apply {
            shown = listOf("a", "c", "d")
            visible = setOf(0, 1)
            scrollGate = CompletableDeferred()
            afterScroll = { visible = visible + it }
        }
        var run = start(page, removal("b"))
        change { }
        change { page.shown = listOf("a", "d") }
        change { page.scrollGate!!.complete(Unit) }
        assertTrue(withTimeout(1_000) { run.await() })
        assertEquals(listOf<String?>("d"), page.focused)

        // A move down out of view: the displayed rows, then the layout, catch up with the reload one after the
        // other. Nothing is decided on the old places, where the row was still in view.
        page = Page().apply {
            visible = setOf(0, 1, 2, 3)
            afterScroll = { visible = visible + it }
        }
        run = start(page) { shown ->
            when {
                "c" !in shown -> Target.Skip
                shown.indexOf("c") != 3 -> Target.Wait
                else -> Target.Go("c")
            }
        }
        change { }
        change {
            page.laid = page.shown
            page.shown = listOf("a", "b", "d", "c")
        }
        assertEquals(emptyList<Int>(), page.scrolls)
        assertEquals(emptyList<String?>(), page.focused)
        change { page.laid = null }
        assertTrue(withTimeout(1_000) { run.await() })
        assertEquals(listOf(4), page.scrolls)
        assertEquals(listOf<String?>("c"), page.focused)

        // Validity lapses while a scroll is pending: it is cancelled before it changes anything.
        for (lapse in listOf<() -> Unit>({ attention++ }, { exploring = false })) {
            page = Page().apply {
                visible = setOf(0)
                scrollGate = CompletableDeferred()
            }
            run = start(page) { Target.Go("c") }
            change { }
            change(lapse)
            change { page.scrollGate!!.complete(Unit) }
            assertFalse(withTimeout(1_000) { run.await() })
            assertEquals(emptyList<Int>(), page.scrolls)
            assertEquals(emptyList<String?>(), page.focused)
            change {
                attention = 0
                exploring = true
            }
        }
    }
}
