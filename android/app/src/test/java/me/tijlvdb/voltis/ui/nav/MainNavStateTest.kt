package me.tijlvdb.voltis.ui.nav

import androidx.compose.runtime.saveable.SaverScope
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MainNavStateTest {
    private fun MainNavState.dests() = entries.map { it.dest }
    private fun MainNavState.stack(tab: Tab) = stacks.getValue(tab).map { it.dest }

    @Test
    fun tabsKeepTheirStacks() {
        val nav = MainNavState()
        assertEquals(listOf(LibrariesDest, SearchDest, SettingsDest, HomeDest), nav.dests())

        nav.open(ContentDest("a"))
        nav.select(Tab.Libraries)
        nav.open(LibraryDest("l"))
        nav.open(ContentDest("a"))
        // The other tabs first, Home last of them, then the current tab.
        assertEquals(
            listOf(SearchDest, SettingsDest, HomeDest, ContentDest("a"), LibrariesDest, LibraryDest("l"), ContentDest("a")),
            nav.dests(),
        )
        // The same destination in two tabs is two entries.
        assertEquals(nav.entries.size, nav.entries.distinct().size)

        nav.select(Tab.Home)
        assertEquals(listOf(HomeDest, ContentDest("a")), nav.stack(Tab.Home))
        assertEquals(listOf(LibrariesDest, LibraryDest("l"), ContentDest("a")), nav.stack(Tab.Libraries))

        // Reselecting pops to the root, and keeps the root entry.
        val root = nav.stacks.getValue(Tab.Home).first()
        nav.select(Tab.Home)
        assertEquals(listOf(root), nav.stacks.getValue(Tab.Home).toList())
    }

    @Test
    fun readerAndBack() {
        val nav = MainNavState()
        nav.select(Tab.Search)
        nav.open(ContentDest("series"))
        nav.openReader("v1")
        assertEquals(ReaderDest("v1"), nav.dests().last())

        // Another volume replaces the reader entry instead of stacking.
        val first = nav.reader
        nav.openReader("v2")
        assertNotEquals(first, nav.reader)
        assertEquals(1, nav.dests().count { it is ReaderDest })

        // "Details" closes the reader and opens the page in the tab it came from.
        nav.closeReader(thenOpen = ContentDest("v2"))
        assertNull(nav.reader)
        assertEquals(listOf(SearchDest, ContentDest("series"), ContentDest("v2")), nav.stack(Tab.Search))

        // Back: the reader, then the tab's stack, then Home, where it does nothing more.
        nav.openReader("v2")
        nav.back()
        assertNull(nav.reader)
        nav.back()
        assertEquals(listOf(SearchDest, ContentDest("series")), nav.stack(Tab.Search))
        nav.back()
        assertEquals(listOf(SearchDest), nav.stack(Tab.Search))
        nav.back()
        assertEquals(Tab.Home, nav.current)
        val home = nav.entries
        nav.back()
        assertEquals(home, nav.entries)
    }

    @Test
    fun clearAndRestore() {
        val nav = MainNavState()
        nav.select(Tab.Settings)
        nav.open(SessionsDest)
        nav.select(Tab.Libraries)
        nav.open(BrowseDest(sort = "continue", sortOrder = "desc"))
        nav.openReader("v1")

        val saved = with(MainNavState.Saver) { SaverScope { true }.save(nav) }!!
        val restored = MainNavState.Saver.restore(saved)!!
        assertEquals(nav.entries, restored.entries)
        assertEquals(Tab.Libraries, restored.current)
        assertEquals(nav.reader, restored.reader)
        // New entries don't reuse a restored one's uid.
        restored.open(ContentDest("a"))
        assertEquals(restored.entries.size, restored.entries.map { it.uid }.distinct().size)

        // State that no longer decodes starts over instead of crashing.
        val unknown = saved.replace("SessionsDest", "RemovedDest")
        assertNotEquals(saved, unknown)
        assertNull(MainNavState.Saver.restore(unknown))
        assertNull(MainNavState.Saver.restore("{"))

        val old = nav.entries.toSet()
        nav.clear()
        assertEquals(Tab.Home, nav.current)
        assertEquals(listOf(LibrariesDest, SearchDest, SettingsDest, HomeDest), nav.dests())
        // Every old entry is gone, roots included, so their view models are cleared.
        assertTrue(nav.entries.none { it in old })
    }
}
