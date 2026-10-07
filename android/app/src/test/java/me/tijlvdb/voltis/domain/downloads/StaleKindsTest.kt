package me.tijlvdb.voltis.domain.downloads

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** The pure rules; [StaleTest] has the refresh and its observation on a real database. */
class StaleKindsTest {
    @Test
    fun staleKinds() {
        // The manifest's time is UTC, the API's has the server's offset: the same instant isn't stale.
        val utc = "2026-01-10T06:37:12.648809Z"
        val local = "2026-01-10T10:37:12.648809+04:00"
        assertNull(staleOf(utc, 100, ServerFile(true, local, 100)))
        assertEquals(Stale.VERSION, staleOf(utc, 100, ServerFile(true, "2026-01-10T10:37:13+04:00", 100)))
        assertEquals(Stale.VERSION, staleOf(utc, 100, ServerFile(true, local, 101)))
        // Missing from the list (or a 404), or no longer valid.
        assertEquals(Stale.GONE, staleOf(utc, 100, null))
        assertEquals(Stale.GONE, staleOf(utc, 100, ServerFile(false, local, 100)))
        // Unknown on one side is no difference.
        assertNull(staleOf(null, 100, ServerFile(true, local, null)))
    }

    @Test
    fun filesIntactKinds() {
        // Three pages of 10 bytes, and a manifest and a cover that bring the directory to 40.
        val lengths = mapOf(0 to 10L, 1 to 10L, 2 to 10L)
        fun intact(pages: Map<Int, Long?> = lengths, copyBytes: Long = 40, dirBytes: Long = 40) = filesIntact(3, copyBytes, { pages[it] }, dirBytes)
        assertTrue(intact())
        assertFalse("a page missing", intact(lengths - 1))
        assertFalse("a page empty", intact(lengths + (2 to 0L)))
        assertFalse("a page truncated", intact(lengths + (0 to 4L), dirBytes = 34))
        assertFalse("the directory short of copy_bytes", intact(dirBytes = 39))
        assertTrue("copy_bytes unknown is not compared", intact(copyBytes = 0, dirBytes = 12))
    }
}
