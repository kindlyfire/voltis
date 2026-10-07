package me.tijlvdb.voltis.domain.reading

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ComicProgressTest {
    private fun json(text: String) = Json.parseToJsonElement(text) as JsonObject

    @Test
    fun positionsAndTheFinish() {
        assertEquals(json("""{"current_page":0,"progress_percent":0}"""), position(0, 40))
        assertEquals(json("""{"current_page":1,"progress_percent":33.3}"""), position(1, 3))
        // Halves round up, as JavaScript's Math.round does: 1/16 is 6.25 %.
        assertEquals(json("""{"current_page":1,"progress_percent":6.3}"""), position(1, 16))
        // Divided first, as the web does: 201 / 400 * 1000 is 502.49999… there.
        assertEquals(json("""{"current_page":201,"progress_percent":50.2}"""), position(201, 400))
        // Only a finish reaches 100 %.
        assertEquals(json("""{"current_page":1999,"progress_percent":99.9}"""), position(1999, 2000))
        assertEquals(json("""{"current_page":3}"""), position(3, 0))
        assertEquals(json("""{"current_page":39,"progress_percent":100,"at_end":true}"""), finishProgress(40))
    }

    @Test
    fun positionsCompareByTheirPage() {
        val adapter = ComicAdapter(pages = { 40 })
        // The end is the last page, wherever its current_page says.
        assertTrue(adapter.samePosition(json("""{"at_end":true}"""), position(39, 40)))
        assertTrue(adapter.samePlace(finishProgress(40), json("""{"current_page":39}""")))
        assertFalse(adapter.samePosition(finishProgress(40), position(38, 40)))
        // Past the end is clamped; nothing is page 0.
        assertTrue(adapter.samePosition(json("""{"current_page":90}"""), position(39, 40)))
        assertTrue(adapter.samePosition(EmptyProgress, position(0, 40)))
        assertEquals(PositionLabel.Page(12), adapter.describe(position(11, 40)))
    }
}
