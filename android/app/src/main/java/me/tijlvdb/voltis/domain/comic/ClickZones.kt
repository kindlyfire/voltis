package me.tijlvdb.voltis.domain.comic

import kotlin.math.min

enum class ClickZone { Prev, Next, Menu }

/** The share of the height that the previous band and the next band each take. */
const val HEIGHT_ZONE = 0.3f

/**
 * The zone of a tap at ([x], [y]) in a [width] by [height] reader: the top and bottom bands are
 * previous and next, the sides of the middle band too, and its centre column, at most [centerCap]
 * wide, is the menu. [flipped] swaps the sides; the bands stay.
 */
fun getClickZone(x: Float, y: Float, width: Float, height: Float, centerCap: Float, flipped: Boolean): ClickZone {
    val center = min(width / 3, centerCap)
    val left = (width - center) / 2
    return when {
        y < height * HEIGHT_ZONE -> ClickZone.Prev
        y > height * (1 - HEIGHT_ZONE) -> ClickZone.Next
        x < left -> if (flipped) ClickZone.Next else ClickZone.Prev
        x > left + center -> if (flipped) ClickZone.Prev else ClickZone.Next
        else -> ClickZone.Menu
    }
}

/**
 * Whether a swipe may turn the page, as `handleTouchEnd` in `useReaderControls.ts`: only from both
 * the edge the finger reveals and the reading-order edge for the move, so a pan that reaches an
 * edge doesn't also turn. [toRight] is a swipe that reveals what is further right; [atLeft] and
 * [atRight] say which physical edges the spread is at (both, when it fits). In RTL without
 * flipped controls the two edges are opposite: an overflowing spread only pans.
 */
fun swipeTurns(toRight: Boolean, flipped: Boolean, rtl: Boolean, atLeft: Boolean, atRight: Boolean): Boolean {
    val next = toRight != flipped
    val readingEdgeIsRight = next != rtl
    return (if (toRight) atRight else atLeft) && (if (readingEdgeIsRight) atRight else atLeft)
}
