package me.tijlvdb.voltis.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.lacksLocalNetwork
import me.tijlvdb.voltis.ui.kit.OfflineBanner
import me.tijlvdb.voltis.ui.kit.OfflineState
import okhttp3.HttpUrl

/** What the signed-in frame gives its screens for offline mode (P2 §10). [probe] says whether the server answered. */
class Offline(val online: StateFlow<Boolean>, val probe: suspend () -> Boolean, val server: () -> HttpUrl?, val openDownloads: () -> Unit)

/** Outside the signed-in frame (previews, the kit) everything counts as online. */
val LocalOffline = staticCompositionLocalOf { Offline(MutableStateFlow(true), { true }, { null }, {}) }

/** Whether the server can be reached; each screen reloads once when it changes (Refresher). */
@Composable
fun isOnline(): Boolean = LocalOffline.current.online.collectAsStateWithLifecycle().value

/** "Needs a connection" while offline, else null: the reason of a [me.tijlvdb.voltis.ui.kit.VDisabled] control. */
@Composable
fun needsConnection(): String? = if (isOnline()) null else stringResource(R.string.error_needs_connection)

/**
 * Retry, at most one probe at a time from here. When the probe passes while already online, the
 * screen's own failure brought it here, so [reload] runs; a change of `online` reloads by itself.
 */
@Composable
private fun rememberRetry(reload: (() -> Unit)?): () -> Unit {
    val offline = LocalOffline.current
    val scope = rememberCoroutineScope()
    var retrying by remember { mutableStateOf(false) }
    return {
        if (!retrying) {
            retrying = true
            scope.launch {
                try {
                    val wasOnline = offline.online.value
                    if (offline.probe() && wasOnline) reload?.invoke()
                } finally {
                    retrying = false
                }
            }
        }
    }
}

/**
 * The full-screen offline state, with the way to Downloads unless [downloads] is false (the Libraries
 * tab has its row above). [onRetry] reloads the screen when its own load failed while online. Without
 * Android 17's local-network permission for the server, it says so and Retry asks for it.
 */
@Composable
fun ServerOffline(modifier: Modifier = Modifier, downloads: Boolean = true, onRetry: (() -> Unit)? = null) {
    val offline = LocalOffline.current
    val retry = rememberRetry(onRetry)
    val permitted = rememberLocalNetworkAction(retry)
    val url = offline.server()
    val local = url != null && LocalContext.current.lacksLocalNetwork(url)
    OfflineState(
        { if (local) permitted(url) else retry() },
        offline.openDownloads.takeIf { downloads },
        modifier,
        stringResource(if (local) R.string.error_local_network else R.string.error_unreachable),
    )
}

/** The offline banner while offline, or while [shown] (a page shown from storage), for a page that works without the server. */
@Composable
fun OfflineBar(modifier: Modifier = Modifier, shown: Boolean = false, onRetry: (() -> Unit)? = null) {
    if (!shown && isOnline()) return
    OfflineBanner(rememberRetry(onRetry), modifier)
}

/** [content] while online; offline, the offline state in its place. */
@Composable
fun OnlineOnly(modifier: Modifier = Modifier, downloads: Boolean = true, content: @Composable () -> Unit) {
    if (isOnline()) content() else ServerOffline(modifier, downloads)
}
