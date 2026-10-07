package me.tijlvdb.voltis.domain.comic

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** The settings cases of `ComicDisplay/useComicDisplayStore.test.ts`. */
class ReaderSettingsTest {
    @Test
    fun parsesFieldByField() {
        val defaults = ReaderSettings(100, Fit.Screen, SpreadSetting.Auto, zoomWide = true, invertRtlControls = true)
        for (stored in listOf(null, "", "not json", "[]", "{}")) assertEquals(stored, defaults, ReaderSettings.parse(stored))

        // One bad value doesn't discard the rest.
        assertEquals(
            defaults.copy(longstripWidth = 50, seriesSettings = mapOf("s_1" to SeriesSettings(ReaderMode.Paged, null))),
            ReaderSettings.parse("""{"longstripWidth": 50, "fit": "sideways", "seriesSettings": {"s_1": {"mode": "paged"}}}"""),
        )
        assertEquals(
            ReaderSettings(
                100,
                Fit.Width,
                SpreadSetting.Double,
                zoomWide = false,
                invertRtlControls = false,
                mapOf("s_1" to SeriesSettings(ReaderMode.Longstrip, ReadingDirection.Rtl), "c_9" to SeriesSettings(null, ReadingDirection.Ltr)),
                setOf("c_1"),
            ),
            ReaderSettings.parse(
                """{"longstripWidth": 5, "fit": "width", "spread": "double", "zoomWide": false, "invertRtlControls": false,
                "seriesSettings": {"s_1": {"mode": "longstrip", "direction": "rtl"}, "c_9": {"mode": "upside", "direction": "ltr"}},
                "shiftedBooks": {"c_1": true}}""",
            ),
        )
        // A record with a value of the wrong type is dropped whole.
        assertEquals(defaults, ReaderSettings.parse("""{"seriesSettings": {"s_1": 3}, "shiftedBooks": {"c_1": true, "c_2": false}}"""))
    }

    @Test
    fun seriesSettingsKeepADirectionAcrossAutoMode() {
        val stored = ReaderSettings.parse("""{"seriesSettings": {"s_1": {"mode": "paged"}}}""")
        val auto = stored
            .updateSeriesSettings("s_1") { it.copy(direction = ReadingDirection.Rtl) }
            .updateSeriesSettings("s_1") { it.copy(mode = null) }
        assertEquals(mapOf("s_1" to SeriesSettings(null, ReadingDirection.Rtl)), auto.seriesSettings)
        // An entry back on Auto is dropped; others stay.
        val other = auto.updateSeriesSettings("c_9") { it.copy(mode = ReaderMode.Longstrip) }
        assertEquals(setOf("c_9"), other.updateSeriesSettings("s_1") { it.copy(direction = null) }.seriesSettings.keys)

        // What is stored reads back the same, null fields included.
        val all = ReaderSettings(55, Fit.Height, SpreadSetting.Single, zoomWide = false, invertRtlControls = false, other.seriesSettings, setOf("c_1"))
        for (settings in listOf(ReaderSettings(), all)) assertEquals(settings, ReaderSettings.parse(settings.encode()))
        assertTrue(all.encode(), """"s_1":{"mode":null,"direction":"rtl"}""" in all.encode())
    }

    @Test
    fun detectMode() {
        val strip = PageDimensions(800, 3000)
        val unknown = PageDimensions(0, 0)
        // Paged without known sizes.
        assertEquals(ReaderMode.Paged, detectMode(emptyList()))
        assertEquals(ReaderMode.Paged, detectMode(listOf(unknown)))
        // Only known sizes are averaged.
        assertEquals(ReaderMode.Longstrip, detectMode(listOf(strip, unknown)))
        assertEquals(ReaderMode.Longstrip, detectMode(listOf(strip, PageDimensions(800, 1000))))
        assertEquals(ReaderMode.Paged, detectMode(listOf(PageDimensions(800, 1200), unknown)))
    }
}
