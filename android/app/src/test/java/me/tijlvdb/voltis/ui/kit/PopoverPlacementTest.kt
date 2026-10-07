package me.tijlvdb.voltis.ui.kit

import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntRect
import androidx.compose.ui.unit.IntSize
import org.junit.Assert.assertEquals
import org.junit.Test

class PopoverPlacementTest {
    @Test
    fun placePopover() {
        // A 1000×1000 window with a 50 px status bar and a 40 px navigation bar.
        val safe = IntRect(0, 50, 1000, 960)
        val size = IntSize(300, 400)
        data class Case(val name: String, val anchor: IntRect, val safe: IntRect, val rtl: Boolean, val offset: IntOffset, val room: Int)
        val cases = listOf(
            Case("below, room up to the navigation bar", IntRect(100, 100, 148, 148), safe, false, IntOffset(100, 152), 960 - 148 - 4 - 8),
            Case("above when more room there", IntRect(100, 800, 148, 848), safe, false, IntOffset(100, 396), 800 - 50 - 4 - 8),
            Case("below when the minimum is left", IntRect(100, 500, 148, 548), safe, false, IntOffset(100, 552), 960 - 548 - 4 - 8),
            Case("clamped at the end edge", IntRect(900, 100, 948, 148), safe, false, IntOffset(1000 - 8 - 300, 152), 800),
            Case("RTL: the anchor's end edge", IntRect(800, 100, 848, 148), safe, true, IntOffset(548, 152), 800),
            Case("RTL clamped at the start edge", IntRect(10, 100, 58, 148), safe, true, IntOffset(8, 152), 800),
            Case("side insets", IntRect(0, 100, 48, 148), IntRect(60, 50, 940, 960), false, IntOffset(68, 152), 800),
            Case("above, kept under the status bar", IntRect(100, 300, 148, 348), IntRect(0, 50, 1000, 400), false, IntOffset(100, 58), 238),
            // No room either way: a height cap of zero, never a negative one.
            Case("no room", IntRect(100, 60, 148, 90), IntRect(0, 50, 1000, 100), false, IntOffset(100, 94), 0),
        )
        for (case in cases) {
            val spot = placePopover(case.anchor, case.safe, size, case.rtl, gap = 4, margin = 8, minBelow = 400)
            assertEquals(case.name, PopoverSpot(case.offset, case.room), spot)
        }
    }
}
