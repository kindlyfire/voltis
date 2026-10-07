package me.tijlvdb.voltis.domain.comic

import org.junit.Assert.assertEquals
import org.junit.Test

class ClickZonesTest {
    @Test
    fun zones() {
        data class Case(val x: Float, val y: Float, val width: Float, val flipped: Boolean, val expected: ClickZone)
        val cases = listOf(
            // The bands are 30% of the height each, run the full width, and win over the sides.
            Case(850f, 299f, 900f, false, ClickZone.Prev),
            Case(450f, 301f, 900f, false, ClickZone.Menu),
            Case(50f, 701f, 900f, false, ClickZone.Next),
            Case(450f, 699f, 900f, false, ClickZone.Menu),
            // The middle band: thirds.
            Case(299f, 500f, 900f, false, ClickZone.Prev),
            Case(301f, 500f, 900f, false, ClickZone.Menu),
            Case(599f, 500f, 900f, false, ClickZone.Menu),
            Case(601f, 500f, 900f, false, ClickZone.Next),
            // Flipped swaps the sides only.
            Case(100f, 500f, 900f, true, ClickZone.Next),
            Case(800f, 500f, 900f, true, ClickZone.Prev),
            Case(450f, 500f, 900f, true, ClickZone.Menu),
            Case(100f, 100f, 900f, true, ClickZone.Prev),
            Case(800f, 900f, 900f, true, ClickZone.Next),
            // A third of 2400 would be 800: the centre is capped at 600, from 900 to 1500.
            Case(899f, 500f, 2400f, false, ClickZone.Prev),
            Case(901f, 500f, 2400f, false, ClickZone.Menu),
            Case(1499f, 500f, 2400f, false, ClickZone.Menu),
            Case(1501f, 500f, 2400f, false, ClickZone.Next),
        )
        for (case in cases) {
            assertEquals("$case", case.expected, getClickZone(case.x, case.y, case.width, height = 1000f, centerCap = 600f, flipped = case.flipped))
        }
    }

    @Test
    fun swipeTurns() {
        data class Case(val name: String, val rtl: Boolean, val flipped: Boolean, val atLeft: Boolean, val atRight: Boolean, val toRight: Boolean, val toLeft: Boolean)
        val cases = listOf(
            Case("a spread that fits turns both ways", rtl = false, flipped = false, atLeft = true, atRight = true, toRight = true, toLeft = true),
            Case("LTR at its start turns back only", rtl = false, flipped = false, atLeft = true, atRight = false, toRight = false, toLeft = true),
            Case("LTR at its end turns on only", rtl = false, flipped = false, atLeft = false, atRight = true, toRight = true, toLeft = false),
            Case("mid-pan never turns", rtl = false, flipped = false, atLeft = false, atRight = false, toRight = false, toLeft = false),
            Case("flipped RTL at its start (right) turns back only", rtl = true, flipped = true, atLeft = false, atRight = true, toRight = true, toLeft = false),
            Case("flipped RTL at its end (left) turns on only", rtl = true, flipped = true, atLeft = true, atRight = false, toRight = false, toLeft = true),
            // The revealed edge and the reading edge are opposite: an overflowing spread only pans.
            Case("plain RTL at its start", rtl = true, flipped = false, atLeft = false, atRight = true, toRight = false, toLeft = false),
            Case("plain RTL at its end", rtl = true, flipped = false, atLeft = true, atRight = false, toRight = false, toLeft = false),
            Case("plain RTL that fits", rtl = true, flipped = false, atLeft = true, atRight = true, toRight = true, toLeft = true),
        )
        for (case in cases) {
            assertEquals(case.name, case.toRight, swipeTurns(true, case.flipped, case.rtl, case.atLeft, case.atRight))
            assertEquals(case.name, case.toLeft, swipeTurns(false, case.flipped, case.rtl, case.atLeft, case.atRight))
        }
    }
}
