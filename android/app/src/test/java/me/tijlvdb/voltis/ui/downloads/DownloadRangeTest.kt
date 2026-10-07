package me.tijlvdb.voltis.ui.downloads

import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.domain.downloads.DownloadState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DownloadRangeTest {
    private fun volumes(count: Int, done: Int = 0) = List(count) { i ->
        Content("v$i", "v$i", ContentType.COMIC, fileSize = 10L, userData = if (i < done) UserData(status = ReadingStatus.COMPLETED) else null)
    }

    @Test
    fun theCurrentVolumeIsTheContinueTargetThenTheFirstUnreadThenTheFirst() {
        val list = volumes(10, done = 4)
        assertEquals(7, currentIndex(list, "v7"))
        assertEquals(4, currentIndex(list, null))
        assertEquals(4, currentIndex(list, "gone"))
        assertEquals(0, currentIndex(volumes(3, done = 3), null))
        assertNull(currentIndex(emptyList(), null))
        assertEquals(RangeSel(4, 9), defaultRange(list, 4))
    }

    @Test
    fun shortcutsFillTheRange() {
        val list = volumes(10, done = 4)
        assertEquals(RangeSel(0, 9), shortcutRange(Shortcut.All, list, 6))
        assertEquals(RangeSel(4, 9), shortcutRange(Shortcut.Unread, list, 6))
        assertEquals(RangeSel(6, 9), shortcutRange(Shortcut.Next, list, 6))
        assertEquals(RangeSel(2, 6), shortcutRange(Shortcut.Next, list, 2))
        assertNull(shortcutRange(Shortcut.Unread, volumes(3, done = 3), 0))
    }

    @Test
    fun toOnlyFollowsFrom() {
        val list = volumes(10)
        assertEquals((6..9).toList(), toOptions(list, 5).toList())
        assertTrue(toOptions(list, 9).isEmpty())
        // Picking From past To moves To to the last; at or before From, To is refused.
        assertEquals(RangeSel(8, 9), RangeSel(2, 5).withFrom(8, list))
        assertEquals(RangeSel(3, 5), RangeSel(2, 5).withFrom(3, list))
        assertEquals(RangeSel(2, 5), RangeSel(2, 5).withTo(2))
        assertEquals(RangeSel(2, 7), RangeSel(2, 5).withTo(7))
    }

    @Test
    fun theRangeSkipsWhatHasADownloadAndCountsIt() {
        val states = mapOf("v3" to DownloadState.DONE, "v7" to DownloadState.QUEUED, "v5" to DownloadState.FAILED, "v9" to DownloadState.DONE)
        val plan = rangePlan(volumes(10), RangeSel(2, 7), states)
        // The store's enqueue leaves a failed row alone, so it is neither downloaded nor "on this phone".
        assertEquals(listOf("v2", "v4", "v6"), plan.items.map { it.id })
        assertEquals(2, plan.present)
        assertEquals(1, plan.stuck)
        assertEquals(30L, plan.bytes)
        assertEquals(0, rangePlan(volumes(10), RangeSel(), emptyMap()).count)
        assertTrue(rangePlan(volumes(600), RangeSel(0, 599), emptyMap()).tooMany)
    }
}
