package me.tijlvdb.voltis.ui

import android.Manifest
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.exclude
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.isImeVisible
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.lacksLocalNetwork
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VSpinner
import me.tijlvdb.voltis.ui.kit.VTopBar
import me.tijlvdb.voltis.ui.theme.VoltisShapes
import me.tijlvdb.voltis.ui.theme.VoltisTheme
import okhttp3.HttpUrl

/**
 * A sign-in page, the web's `AuthCard`: the wordmark over a card of at most 420 dp, both centred in
 * the window, with the page's heading and [subtitle] at the card's top. It scrolls when taller.
 * While the keyboard is up they sit at the top, so the card doesn't slide as the keyboard moves.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun SignInScreen(title: String, subtitle: String? = null, content: @Composable ColumnScope.() -> Unit) {
    Surface(Modifier.fillMaxSize()) {
        // The keyboard's inset goes on the scrolling column, so that it, not the page, shrinks for it.
        BoxWithConstraints(Modifier.windowInsetsPadding(WindowInsets.safeDrawing.exclude(WindowInsets.ime))) {
            val ime = WindowInsets.isImeVisible
            Column(
                Modifier
                    .imePadding()
                    .verticalScroll(rememberScrollState())
                    .heightIn(min = if (ime) 0.dp else maxHeight)
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = if (ime || isShort()) 16.dp else 40.dp),
                Arrangement.spacedBy(24.dp, if (ime) Alignment.Top else Alignment.CenterVertically),
                Alignment.CenterHorizontally,
            ) {
                // Decorative, as on the web: the heading names the page.
                Text(stringResource(R.string.app_name), Modifier.clearAndSetSemantics {}, style = MaterialTheme.typography.titleLarge)
                val colors = VoltisTheme.colors
                Column(
                    Modifier
                        .widthIn(max = 420.dp)
                        .fillMaxWidth()
                        .background(colors.raised, VoltisShapes.card)
                        .border(1.dp, colors.cardLine, VoltisShapes.card)
                        .padding(horizontal = 22.dp, vertical = 20.dp),
                    Arrangement.spacedBy(16.dp),
                ) {
                    Column {
                        Text(title, Modifier.semantics { heading() }, style = MaterialTheme.typography.headlineMedium)
                        if (subtitle != null) {
                            Text(subtitle, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyLarge)
                        }
                    }
                    content()
                }
            }
        }
    }
}

/** A pushed screen: the top bar with Back over a scrolling column. */
@Composable
fun PushedScreen(title: String, onBack: () -> Unit, content: @Composable ColumnScope.() -> Unit) {
    Surface(Modifier.fillMaxSize()) {
        Column {
            VTopBar(title, onBack)
            Column(
                Modifier.verticalScroll(rememberScrollState()).readableWidth(pageGutter()).padding(horizontal = pageGutter()).padding(bottom = bottomSpace()),
                Arrangement.spacedBy(16.dp),
                content = content,
            )
        }
    }
}

/** What stands in for data that isn't there: [error] with Retry, else a spinner while [loading]. */
@Composable
fun LoadStatus(loading: Boolean, error: UiText?, retry: () -> Unit) {
    when {
        error != null -> QueryError(error, retry = retry)
        loading -> VSpinner()
    }
}

/** [content] for a loaded [value]; before that a spinner, and on [error] the message with Retry. */
@Composable
fun <T : Any> Loaded(value: T?, error: UiText?, retry: () -> Unit, content: @Composable (T) -> Unit) {
    if (value != null && error == null) content(value) else LoadStatus(value == null, error, retry)
}

/**
 * Wraps [action] to first ask for local-network access when the URL it's called with needs it.
 * [action] runs either way; after a refusal its request fails with the reason.
 */
@Composable
fun rememberLocalNetworkAction(action: () -> Unit): (HttpUrl?) -> Unit {
    val context = LocalContext.current
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { action() }
    return { url ->
        if (url != null && context.lacksLocalNetwork(url)) {
            launcher.launch(Manifest.permission.ACCESS_LOCAL_NETWORK)
        } else {
            action()
        }
    }
}
