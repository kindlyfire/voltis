package me.tijlvdb.voltis.ui

import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.domain.layout.TALL_DP
import me.tijlvdb.voltis.domain.layout.WIDE_DP
import me.tijlvdb.voltis.domain.layout.WIDE_GUTTER_DP

/** The window's width (not the screen's, so split-screen follows). */
@Composable
fun windowWidthDp(): Float = with(LocalDensity.current) { LocalWindowInfo.current.containerSize.width.toDp().value }

@Composable
private fun windowHeightDp(): Float = with(LocalDensity.current) { LocalWindowInfo.current.containerSize.height.toDp().value }

/** A window wide enough for the rail and the wide single-pane layouts. */
@Composable
fun isWide(): Boolean = windowWidthDp() >= WIDE_DP

/** A wide window that is tall enough for the larger titles and cards; a phone on its side isn't. */
@Composable
fun isLarge(): Boolean = isWide() && windowHeightDp() >= TALL_DP

/** A wide window too short for the large sizes: a phone on its side. */
@Composable
fun isShort(): Boolean = isWide() && !isLarge()

/** The page's side margin, the web's `page-frame`: 16 dp, 40 dp on a wide window. */
@Composable
fun pageGutter(): Dp = if (windowWidthDp() >= WIDE_GUTTER_DP) 40.dp else 16.dp

/** The margin before an icon button at a row's end: its glyph, 12 dp inside its target, ends on the gutter. */
@Composable
fun pageGutterBeforeIcon(): Dp = pageGutter() - 12.dp

/**
 * The side insets (cutouts, a navigation bar at the side) of a signed-in screen. Each entry but
 * the reader's pads itself, so the reader can still draw over the whole window.
 */
@Composable
fun Modifier.entryInsets(): Modifier = windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal))

/** What a tab root pads at its top in place of a top bar. */
@Composable
fun Modifier.topInset(): Modifier = windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Top))

/**
 * What a scrolling screen must leave at its end for the system's navigation bar: none above the
 * bottom bar, which already clears it; its height beside the rail, where content reaches the
 * window's bottom edge.
 */
val LocalBottomInset = compositionLocalOf { 0.dp }

/**
 * How far the snackbars sit above their usual place: a bar over the content's bottom (the grid's
 * selection bar) reports its height here, so a snackbar doesn't cover it.
 */
val LocalSnackbarLift = staticCompositionLocalOf { mutableStateOf(0.dp) }

/** The space at the end of a scrolling container. */
@Composable
fun bottomSpace(): Dp = 24.dp + LocalBottomInset.current

/**
 * A column of text and controls at a readable width, at the start of a wider window as the web's
 * pages are. The width leaves room for the page's [gutter] (`pageGutter()`), which the caller pads.
 */
fun Modifier.readableWidth(gutter: Dp): Modifier = startColumn(688.dp + gutter * 2)

/** The web's `max-w-[960px]` page of a table or a list of entries: 960 dp, gutters included. */
fun Modifier.wideReadableWidth(): Modifier = startColumn(960.dp)

private fun Modifier.startColumn(max: Dp): Modifier = fillMaxWidth().wrapContentWidth(Alignment.Start).widthIn(max = max).fillMaxWidth()

/** A select or a field in a page's row of controls: the web's `sm:w-56`, which grows with the font size. */
@Composable
fun controlWidth(): Dp = 224.dp * LocalDensity.current.fontScale.coerceIn(1f, 1.5f)
