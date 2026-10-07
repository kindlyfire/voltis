package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.theme.VoltisShapes

/** A failed request or action: the message, announced politely, with Retry when [retry] is given. */
@Composable
fun QueryError(text: UiText?, modifier: Modifier = Modifier, retry: (() -> Unit)? = null) {
    if (text == null) return
    val colors = MaterialTheme.colorScheme
    Row(
        modifier
            .fillMaxWidth()
            .background(colors.errorContainer, VoltisShapes.toast)
            .padding(start = 16.dp, end = if (retry == null) 16.dp else 4.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(VIcons.AlertCircle, contentDescription = null, tint = colors.onErrorContainer)
        Text(
            text.resolve(),
            Modifier.weight(1f).padding(vertical = 14.dp).semantics { liveRegion = LiveRegionMode.Polite },
            color = colors.onErrorContainer,
            style = MaterialTheme.typography.bodyMedium,
        )
        if (retry != null) VButton(stringResource(R.string.retry), retry, style = VButtonStyle.Text)
    }
}
