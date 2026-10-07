package me.tijlvdb.voltis.domain.layout

import org.junit.Assert.assertEquals
import org.junit.Test

class LayoutRulesTest {
    @Test
    fun navKindFollowsTheReaderAndTheWidth() {
        val cases = listOf(
            Triple(true, 1280f, NavKind.None),
            Triple(false, 599f, NavKind.Bar),
            Triple(false, 600f, NavKind.Rail),
            Triple(false, 1280f, NavKind.Rail),
        )
        for ((reader, width, kind) in cases) assertEquals("reader=$reader at $width", kind, navKind(reader, width))
    }
}
