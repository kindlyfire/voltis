package me.tijlvdb.voltis.domain.comic

import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt

/** The longest edge a spread page is decoded at: about the GPU's texture limit. */
private const val MAX_DECODE_EDGE = 4096.0

/** Spread Auto: two pages when the viewport is wider than tall. */
fun resolveDouble(spread: SpreadSetting, viewport: Extent): Boolean =
    spread == SpreadSetting.Double || spread == SpreadSetting.Auto && viewport.width > viewport.height

/**
 * The size to decode one page of a two-page spread at. [slot] is the size it is laid out at, in
 * px; [scale] what it is shown at. A sized page is fitted into the slot by its own aspect; an
 * unknown one takes the slot. Capped at the page's own size and at 4096 px, keeping the aspect.
 */
fun spreadDecodeSize(slot: Extent, page: PageDimensions, scale: Float): PageDimensions {
    val fit = if (page.sized) min(slot.width / page.width, slot.height / page.height) else 1.0
    val width = (if (page.sized) page.width * fit else slot.width) * scale
    val height = (if (page.sized) page.height * fit else slot.height) * scale
    var factor = min(1.0, MAX_DECODE_EDGE / max(width, height))
    if (page.sized) factor = min(factor, page.width / width)
    return PageDimensions(max(1, (width * factor).roundToInt()), max(1, (height * factor).roundToInt()))
}

/**
 * Whether the settled spread gets its larger decode, at [zoom] times the fit: from 1.25, and kept
 * down to 1.05 once it has it ([sharp]), so a pinch around one threshold doesn't flicker.
 */
fun spreadSharp(zoom: Float, sharp: Boolean): Boolean = if (sharp) zoom >= 1.05f else zoom > 1.25f
