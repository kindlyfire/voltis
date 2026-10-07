package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
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
import me.tijlvdb.voltis.ui.theme.VoltisShapes

/** "Offline" with Retry, at the top of a page that works without the server (P2 §10). Announced politely. Retry stays enabled while it runs. */
@Composable
fun OfflineBanner(onRetry: () -> Unit, modifier: Modifier = Modifier) {
    val colors = MaterialTheme.colorScheme
    Row(
        modifier
            .fillMaxWidth()
            .background(colors.surfaceContainerHighest, VoltisShapes.toast)
            .padding(start = 16.dp, end = 4.dp),
        Arrangement.spacedBy(12.dp),
        Alignment.CenterVertically,
    ) {
        Icon(VIcons.CloudOff, contentDescription = null, Modifier.size(20.dp), tint = colors.onSurfaceVariant)
        Text(
            stringResource(R.string.offline_banner),
            Modifier.weight(1f).padding(vertical = 12.dp).semantics { liveRegion = LiveRegionMode.Polite },
            color = colors.onSurface,
            style = MaterialTheme.typography.bodyMedium,
        )
        VButton(stringResource(R.string.retry), onRetry, style = VButtonStyle.Text)
    }
}
