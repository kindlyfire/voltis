package me.tijlvdb.voltis.domain.catalog

import org.junit.Assert.assertEquals
import org.junit.Test

class GridColumnsTest {
    @Test
    fun columnsFollowTheWidth() {
        // Width in dp: phones in portrait, a phone in landscape, tablets. The default is 3 columns across the phones.
        data class Case(val width: Float, val atDefault: Int, val at127: Int, val range: IntRange)
        val cases = listOf(
            Case(340f, atDefault = 3, at127 = 3, range = 1..3),
            Case(380f, atDefault = 3, at127 = 3, range = 1..3),
            Case(470f, atDefault = 3, at127 = 4, range = 1..4),
            Case(330f, atDefault = 2, at127 = 3, range = 1..3),
            Case(850f, atDefault = 6, at127 = 7, range = 2..7),
            Case(800f, atDefault = 6, at127 = 6, range = 2..7),
        )
        val default = GridOptions().itemSize
        for (case in cases) {
            assertEquals("${case.width} at default", case.atDefault, gridColumns(case.width, default))
            assertEquals("${case.width} at 127", case.at127, gridColumns(case.width, 127))
            assertEquals("${case.width} range", case.range, columnRange(case.width))
        }
        // A half rounds up, as on the web.
        assertEquals(3, gridColumns(337.5f, default))
        assertEquals(1, gridColumns(100f, default))
        // Three columns chosen on the phone carry over to landscape.
        assertEquals(127, itemSizeFor(380f, 3))
        assertEquals(7, gridColumns(850f, itemSizeFor(380f, 3)))
    }
}
