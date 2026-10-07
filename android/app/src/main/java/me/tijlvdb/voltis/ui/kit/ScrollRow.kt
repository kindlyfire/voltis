package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.ui.isLarge
import me.tijlvdb.voltis.ui.pageGutter

/** The width of an item of a [ScrollRow]: the web's 176 px, at its narrow-screen 0.75 below a large window. */
@Composable
fun scrollRowItemWidth(): Dp = if (isLarge()) 176.dp else 132.dp

/** The gap between the items of a [ScrollRow]. */
val ScrollRowGap = 18.dp

/**
 * A titled row that scrolls sideways (the Home sections). [onSeeAll] is the arrow to the full
 * listing. Items give themselves [scrollRowItemWidth]. [placeholder] stands in for the list while
 * it loads: the list's saved position is then only applied to the loaded items.
 */
@Composable
fun ScrollRow(
    title: String,
    onSeeAll: () -> Unit,
    modifier: Modifier = Modifier,
    placeholder: (@Composable () -> Unit)? = null,
    content: LazyListScope.() -> Unit,
) {
    Column(modifier, Arrangement.spacedBy(6.dp)) {
        Row(Modifier.padding(horizontal = pageGutter()).heightIn(min = 48.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(
                title,
                Modifier.weight(1f, fill = false).padding(end = 4.dp).semantics { heading() },
                style = if (isLarge()) MaterialTheme.typography.headlineMedium else MaterialTheme.typography.headlineSmall,
            )
            VIconButton(VIcons.ArrowRight, stringResource(R.string.see_all, title), onSeeAll, style = VIconButtonStyle.Subtle, small = true)
        }
        val state = rememberLazyListState()
        if (placeholder != null) {
            placeholder()
            return@Column
        }
        // A lazy list follows its first visible item by key. At its start the row stays there
        // instead, so a refresh that puts a new card first shows it.
        SideEffect {
            if (state.firstVisibleItemIndex == 0 && state.firstVisibleItemScrollOffset == 0) state.requestScrollToItem(0)
        }
        LazyRow(
            state = state,
            contentPadding = PaddingValues(horizontal = pageGutter()),
            horizontalArrangement = Arrangement.spacedBy(ScrollRowGap),
            content = content,
        )
    }
}
