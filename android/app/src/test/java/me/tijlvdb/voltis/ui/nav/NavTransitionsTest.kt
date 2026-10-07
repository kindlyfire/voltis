package me.tijlvdb.voltis.ui.nav

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class NavTransitionsTest {
    @Test
    fun motionByTab() {
        assertEquals(NavMotion.Slide, navMotion(Tab.Libraries, Tab.Libraries))
        assertEquals(NavMotion.None, navMotion(Tab.Home, Tab.Libraries))
        assertEquals(NavMotion.None, navMotion(Tab.Search, Tab.Home))
        assertEquals(NavMotion.Fade, navMotion(Tab.Home, null))
        assertEquals(NavMotion.Fade, navMotion(null, Tab.Settings))
        // Pops mirror pushes, and RTL mirrors both.
        assertEquals(1, slideSign(pop = false, rtl = false))
        assertEquals(-1, slideSign(pop = true, rtl = false))
        assertEquals(-1, slideSign(pop = false, rtl = true))
        assertEquals(1, slideSign(pop = true, rtl = true))
    }

    @Test
    fun tabOfEntry() {
        val nav = MainNavState()
        nav.select(Tab.Libraries)
        nav.open(DiscoverDest)
        nav.openReader("c1")
        assertEquals(Tab.Libraries, nav.tabOf(nav.stacks.getValue(Tab.Libraries).last()))
        assertEquals(Tab.Home, nav.tabOf(nav.stacks.getValue(Tab.Home).first()))
        assertNull(nav.tabOf(nav.reader!!))
    }
}
