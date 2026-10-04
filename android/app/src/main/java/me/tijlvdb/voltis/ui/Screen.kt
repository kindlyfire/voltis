package me.tijlvdb.voltis.ui

import android.Manifest
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.lacksLocalNetwork
import okhttp3.HttpUrl

/** A centred, scrollable single-column page with a heading. */
@Composable
fun FormScreen(title: String, subtitle: String? = null, content: @Composable ColumnScope.() -> Unit) {
    Surface(Modifier.fillMaxSize()) {
        Column(
            Modifier
                .safeDrawingPadding()
                .verticalScroll(rememberScrollState())
                .padding(24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Column(Modifier.widthIn(max = 480.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
                Text(
                    title,
                    style = MaterialTheme.typography.headlineMedium,
                    modifier = Modifier.semantics { heading() },
                )
                if (subtitle != null) {
                    Text(
                        subtitle,
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                content()
            }
        }
    }
}

/** [content] for a loaded [value]; before that a spinner, and on [error] the message with Retry. */
@Composable
fun <T : Any> Loaded(value: T?, error: UiText?, retry: () -> Unit, content: @Composable (T) -> Unit) {
    when {
        error != null -> {
            ErrorText(error)
            Button(onClick = retry) { Text(stringResource(R.string.retry)) }
        }
        value == null -> {
            val label = stringResource(R.string.loading)
            CircularProgressIndicator(Modifier.semantics { contentDescription = label })
        }
        else -> content(value)
    }
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

@Composable
fun ErrorText(text: UiText?) {
    if (text != null) {
        Text(
            text.resolve(),
            color = MaterialTheme.colorScheme.error,
            modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite },
        )
    }
}
