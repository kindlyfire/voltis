package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.add
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.ui.pageGutter

/**
 * The bar of a pushed screen: back, a one-line title, [actions]. Its icons line up with the page's
 * gutter. [overlay] is a bar over content (the reader's), on `surface-1`, at the window's edges. [onTitleClick] makes the title a button, named [titleClickLabel].
 * [navIcon] and [navLabel] replace Back (a selection's close button); a [liveTitle] is announced when it changes. Without [back] there is no arrow: a screen nothing leads back from.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun VTopBar(
    title: String,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    overlay: Boolean = false,
    onTitleClick: (() -> Unit)? = null,
    titleClickLabel: String? = null,
    navIcon: Painter = VIcons.ArrowLeft,
    navLabel: String = stringResource(R.string.back),
    liveTitle: Boolean = false,
    back: Boolean = true,
    actions: @Composable RowScope.() -> Unit = {},
) {
    val colors = MaterialTheme.colorScheme
    // An icon's glyph is 16 dp in from the bar's edge, the narrow gutter.
    val extra = if (overlay) 0.dp else pageGutter() - 16.dp
    TopAppBar(
        title = {
            val click = if (onTitleClick == null) {
                Modifier
            } else {
                Modifier.clickable(onClickLabel = titleClickLabel, role = Role.Button, onClick = onTitleClick)
                    .minimumInteractiveComponentSize()
            }
            Text(
                title,
                click.semantics {
                    heading()
                    if (liveTitle) liveRegion = LiveRegionMode.Polite
                },
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        },
        modifier = modifier,
        navigationIcon = { if (back) VIconButton(navIcon, navLabel, onBack) },
        actions = actions,
        windowInsets = TopAppBarDefaults.windowInsets.add(WindowInsets(left = extra, right = extra)),
        colors = TopAppBarDefaults.topAppBarColors(containerColor = if (overlay) colors.surfaceContainerLow else colors.surface),
    )
}
