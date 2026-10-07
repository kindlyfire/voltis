package me.tijlvdb.voltis.ui.grid

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.ui.LocalBottomInset
import me.tijlvdb.voltis.ui.kit.VBarButton
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VTopBar

/** In place of a grid screen's bar while it selects: close, and the count, which TalkBack announces. */
@Composable
fun SelectionTopBar(count: Int, onClose: () -> Unit) {
    VTopBar(
        pluralStringResource(R.plurals.selected_count, count, count),
        onClose,
        navIcon = VIcons.Close,
        navLabel = stringResource(R.string.select_cancel),
        liveTitle = true,
    )
}

/** One of the bar's buttons. A disabled one says [reason]. */
data class BarAction(val icon: Painter, val label: String, val onClick: () -> Unit, val enabled: Boolean = true, val reason: String? = null)

/** The selection's actions, over the grid's bottom, centred at a width a tablet doesn't stretch. */
@Composable
fun SelectionBar(actions: List<BarAction>, modifier: Modifier = Modifier) {
    Surface(modifier.fillMaxWidth(), color = MaterialTheme.colorScheme.surfaceContainerLow) {
        Row(
            // Scrolls sideways when the labels, at a large font, don't fit: a label would otherwise wrap inside a word.
            Modifier.padding(horizontal = 8.dp, vertical = 4.dp).padding(bottom = LocalBottomInset.current).fillMaxWidth().wrapContentWidth().widthIn(max = 480.dp)
                .horizontalScroll(rememberScrollState()),
            Arrangement.spacedBy(8.dp),
        ) {
            for (a in actions) VBarButton(a.icon, a.label, a.onClick, Modifier, a.enabled, a.reason)
        }
    }
}
