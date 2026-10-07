package me.tijlvdb.voltis.domain.layout

/** From this window width the tabs move to a rail and the wide single-pane layouts apply. */
const val WIDE_DP = 600f

/** A wide window needs this height too for the larger titles and cards: a phone on its side has less. */
const val TALL_DP = 480f

/** From this window width the page's gutters are wider, as the web's `page-frame` is from 60rem. */
const val WIDE_GUTTER_DP = 960f

enum class NavKind { None, Bar, Rail }

/** The reader is full-screen above the tabs; otherwise a bar on narrow windows and a rail on wide ones. */
fun navKind(reader: Boolean, widthDp: Float): NavKind = when {
    reader -> NavKind.None
    widthDp < WIDE_DP -> NavKind.Bar
    else -> NavKind.Rail
}
