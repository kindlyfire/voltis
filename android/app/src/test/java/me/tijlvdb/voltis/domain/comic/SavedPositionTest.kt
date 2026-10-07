package me.tijlvdb.voltis.domain.comic

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Test

class SavedPositionTest {
    @Test
    fun pageFor() {
        data class Case(val name: String, val progress: String?, val pages: Int, val expected: Int)
        val cases = listOf(
            Case("saved page", """{"current_page": 1, "progress_percent": 33.3}""", 3, 1),
            Case("a finished comic opens on its last page", """{"current_page": 0, "at_end": true}""", 3, 2),
            Case("past the end is clamped", """{"current_page": 9}""", 3, 2),
            Case("below the start is clamped", """{"current_page": -2}""", 3, 0),
            Case("empty progress", "{}", 3, 0),
            Case("no user data", null, 3, 0),
            Case("a page that isn't a number", """{"current_page": "2"}""", 3, 0),
            Case("no pages", """{"current_page": 4}""", 0, 0),
            Case("no pages, at the end", """{"at_end": true}""", 0, 0),
        )
        for (case in cases) {
            val progress = case.progress?.let { Json.parseToJsonElement(it) as JsonObject }
            assertEquals(case.name, case.expected, pageFor(progress, case.pages))
        }
    }

    @Test
    fun preloadOrder() {
        // The current page, then forward, with at most two pages back.
        assertEquals(listOf(5, 6, 4, 7, 3, 8, 9), getPagesInPreloadOrder(10, 5))
        assertEquals(listOf(0, 1, 2), getPagesInPreloadOrder(3, 0))
        assertEquals(listOf(2, 1, 0), getPagesInPreloadOrder(3, 2))
        assertEquals(emptyList<Int>(), getPagesInPreloadOrder(0, 0))
    }
}
