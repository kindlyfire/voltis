package me.tijlvdb.voltis.ui.reader

import org.junit.Assert.assertEquals
import org.junit.Test

class PagedTurnTest {
    @Test
    fun turnDeltaFollowsTheLivePageSize() {
        // Page 1.25 heading for page 3 (1.75 pages to go), then the pager is resized from 1000 to 600 px a page.
        assertEquals(1750f, turnDelta(3f, 1.25f, 1000), 0.01f)
        assertEquals(1050f, turnDelta(3f, 1.25f, 600), 0.01f)
        // On the page: nothing left.
        assertEquals(0f, turnDelta(3f, 3f, 600), 0f)
    }

    @Test
    fun turns() {
        // Pages 0..3, the end card being 3.
        data class Case(val name: String, val forward: Boolean, val current: Int, val aimed: Int?, val settled: Boolean, val expected: PagedTurn)
        val cases = listOf(
            Case("a page turns", true, 1, null, true, PagedTurn.Go(2)),
            Case("settled on the end card opens the next volume", true, 3, null, true, PagedTurn.NextVolume),
            Case("heading for the end card stops there", true, 3, 3, false, PagedTurn.Go(3)),
            Case("an aimed page counts", true, 2, 3, false, PagedTurn.Go(3)),
            Case("a tap past an aimed end card stops", true, 2, 3, false, PagedTurn.Go(3)),
            Case("scrolling into the end card is not settled", true, 3, null, false, PagedTurn.Go(3)),
            Case("settled on the first page opens the previous volume", false, 0, null, true, PagedTurn.PreviousVolume),
            Case("heading for the first page stops there", false, 1, 0, false, PagedTurn.Go(0)),
            // An interrupt is released after TURN_MILLIS: the aim has lapsed and the pager has settled, but the tap is not settled.
            Case("an interrupt at the end card opens nothing", true, 3, null, false, PagedTurn.Go(3)),
            Case("an interrupt at the first page opens nothing", false, 0, null, false, PagedTurn.Go(0)),
        )
        for (case in cases) {
            assertEquals(case.name, case.expected, pagedTurn(case.forward, case.current, case.aimed, case.settled, 3))
        }
    }
}
