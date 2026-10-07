package me.tijlvdb.voltis.domain.comic

import org.junit.Assert.assertEquals
import org.junit.Test

class SpreadTest {
    @Test
    fun resolveDouble() {
        val wide = Extent(1920.0, 1080.0)
        val tall = Extent(1080.0, 1920.0)
        val cases = listOf(
            Triple(SpreadSetting.Single, wide, false),
            Triple(SpreadSetting.Single, tall, false),
            Triple(SpreadSetting.Double, wide, true),
            Triple(SpreadSetting.Double, tall, true),
            Triple(SpreadSetting.Auto, wide, true),
            Triple(SpreadSetting.Auto, tall, false),
            Triple(SpreadSetting.Auto, Extent(1000.0, 1000.0), false),
        )
        for ((spread, viewport, expected) in cases) assertEquals("$spread $viewport", expected, resolveDouble(spread, viewport))
    }

    @Test
    fun spreadDecodeSize() {
        val slot = Extent(720.0, 1080.0)
        val page = PageDimensions(2000, 3000)
        data class Case(val name: String, val slot: Extent, val page: PageDimensions, val scale: Float, val expected: PageDimensions)
        val cases = listOf(
            Case("the slot at scale 1", slot, page, 1f, PageDimensions(720, 1080)),
            Case("a fit scale of 1.5", slot, page, 1.5f, PageDimensions(1080, 1620)),
            Case("zoomed to three times the fit", slot, page, 4.5f, PageDimensions(2000, 3000)),
            Case("the page's own size", slot, PageDimensions(600, 900), 1f, PageDimensions(600, 900)),
            Case("4096 px, aspect kept", Extent(2000.0, 1000.0), PageDimensions(8000, 4000), 3f, PageDimensions(4096, 2048)),
            Case("a narrow page beside an unknown one, by its own aspect", Extent(960.0, 1080.0), PageDimensions(100, 3000), 1f, PageDimensions(36, 1080)),
            Case("an unknown page size", Extent(960.0, 1080.0), PageDimensions(0, 0), 1.5f, PageDimensions(1440, 1620)),
        )
        for (case in cases) assertEquals(case.name, case.expected, spreadDecodeSize(case.slot, case.page, case.scale))
    }

    @Test
    fun spreadSharp() {
        val cases = listOf(
            Triple(1.25f, false, false),
            Triple(1.3f, false, true),
            Triple(1.1f, true, true),
            Triple(1.04f, true, false),
        )
        for ((zoom, sharp, expected) in cases) assertEquals("$zoom $sharp", expected, spreadSharp(zoom, sharp))
    }
}
