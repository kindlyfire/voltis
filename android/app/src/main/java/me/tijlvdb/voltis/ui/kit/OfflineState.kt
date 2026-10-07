package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
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
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R

/**
 * In place of a screen that needs the server while it can't be reached (P2 §10): "You're offline",
 * why ([reason]), Retry, and with [onDownloads] the way to what works offline. The heading is
 * announced when the state turns up on a screen already open. Retry stays enabled while it runs, so
 * focus stays on it; the caller ignores repeated taps.
 */
@Composable
fun OfflineState(onRetry: () -> Unit, onDownloads: (() -> Unit)?, modifier: Modifier = Modifier, reason: String = stringResource(R.string.error_unreachable)) {
    Column(
        modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 48.dp),
        Arrangement.spacedBy(12.dp),
        Alignment.CenterHorizontally,
    ) {
        Icon(VIcons.CloudOff, contentDescription = null, Modifier.size(40.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(
            stringResource(R.string.offline_title),
            Modifier.semantics {
                heading()
                liveRegion = LiveRegionMode.Polite
            },
            style = MaterialTheme.typography.headlineSmall,
            textAlign = TextAlign.Center,
        )
        Text(
            reason,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
        Column(Modifier.padding(top = 8.dp), Arrangement.spacedBy(8.dp), Alignment.CenterHorizontally) {
            if (onDownloads != null) VButton(stringResource(R.string.offline_downloads), onDownloads, icon = VIcons.Download)
            VButton(stringResource(R.string.retry), onRetry, style = VButtonStyle.Text)
        }
    }
}
