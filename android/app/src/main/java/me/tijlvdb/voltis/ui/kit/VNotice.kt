package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.ui.theme.VoltisShapes
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/**
 * Something on the page that needs a look, with one [action]: "Changed on another device" with
 * Review, or with [warning], "Not saved" with Retry. With a [reason], the action is disabled
 * ([VDisabled]) and the reason also shows under the text. With [live], the text is announced politely:
 * for a notice that turns up on a page already open, not one that opens with it.
 */
@Composable
fun VNotice(
    text: String,
    action: String,
    onAction: () -> Unit,
    modifier: Modifier = Modifier,
    reason: String? = null,
    warning: Boolean = false,
    live: Boolean = true,
) {
    val colors = VoltisTheme.colors
    val content = if (warning) colors.onWarningContainer else colors.onInfoContainer
    Row(
        modifier
            .fillMaxWidth()
            .background(if (warning) colors.warningContainer else colors.infoContainer, VoltisShapes.toast)
            .padding(start = 16.dp, end = 4.dp),
        Arrangement.spacedBy(12.dp),
        Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f).padding(vertical = 12.dp).semantics(mergeDescendants = true) { if (live) liveRegion = LiveRegionMode.Polite }) {
            Text(text, color = content, style = MaterialTheme.typography.bodyMedium)
            if (reason != null) Text(reason, Modifier.clearAndSetSemantics {}, color = content, style = MaterialTheme.typography.bodySmall)
        }
        VDisabled(reason, action) { enabled -> VButton(action, onAction, style = VButtonStyle.Tonal, enabled = enabled) }
    }
}
