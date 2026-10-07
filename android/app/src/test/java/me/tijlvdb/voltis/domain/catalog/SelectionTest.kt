package me.tijlvdb.voltis.domain.catalog

import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SelectionTest {
    private fun item(i: Int, type: String = ContentType.COMIC) = Content("c_$i", "Copper Wren $i", type)

    @Test
    fun togglesCapsAndClears() {
        // A long press enters with the item; a second tap removes it and stays in the mode.
        val one = checkNotNull(Selection().toggle(item(1)))
        assertTrue(one.active)
        assertEquals(Selected(item(1)), one.items["c_1"])
        val none = checkNotNull(one.toggle(item(1)))
        assertTrue(none.active)
        assertTrue(none.items.isEmpty())

        // The 501st item is refused; deselecting still works at the cap.
        var full = Selection(active = true)
        repeat(MAX_SELECTION) { full = checkNotNull(full.toggle(item(it))) }
        assertNull(full.toggle(item(MAX_SELECTION)))
        assertEquals(MAX_SELECTION - 1, checkNotNull(full.toggle(item(0))).items.size)
        // In the order picked, for the Clear dialog's titles.
        assertEquals("c_0", full.items.keys.first())

        // Only a series can have unread volumes to include.
        val volumes = checkNotNull(Selection().toggle(item(1))?.toggle(item(2, ContentType.BOOK)))
        assertFalse(volumes.mayHaveSeries)
        assertTrue(checkNotNull(volumes.toggle(item(3, ContentType.COMIC_SERIES))).mayHaveSeries)

        // A filter change: nothing selected, still selecting.
        val cleared = full.cleared()
        assertTrue(cleared.active)
        assertTrue(cleared.items.isEmpty())
    }
}
