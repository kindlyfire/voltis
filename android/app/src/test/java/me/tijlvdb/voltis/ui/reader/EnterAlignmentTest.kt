package me.tijlvdb.voltis.ui.reader

import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.LayoutDirection
import org.junit.Assert.assertEquals
import org.junit.Test

class EnterAlignmentTest {
    @Test
    fun align() {
        val space = IntSize(1000, 2000)
        val wide = IntSize(1600, 1200)
        val tall = IntSize(1000, 2600)
        data class Case(val name: String, val size: IntSize, val rtl: Boolean, val atEnd: Boolean, val expected: IntOffset)
        val cases = listOf(
            Case("a spread that fits is centred", IntSize(800, 1200), rtl = true, atEnd = true, IntOffset(100, 400)),
            Case("LTR starts at the left", wide, rtl = false, atEnd = false, IntOffset(0, 400)),
            Case("LTR ends at the right", wide, rtl = false, atEnd = true, IntOffset(-600, 400)),
            Case("RTL starts at the right", wide, rtl = true, atEnd = false, IntOffset(-600, 400)),
            Case("RTL ends at the left", wide, rtl = true, atEnd = true, IntOffset(0, 400)),
            Case("a tall page ends at its bottom, whatever the direction", tall, rtl = true, atEnd = true, IntOffset(0, -600)),
        )
        for (case in cases) {
            // The system's layout direction plays no part.
            assertEquals(case.name, case.expected, EnterAlignment(case.atEnd, case.rtl).align(case.size, space, LayoutDirection.Rtl))
        }
    }
}
