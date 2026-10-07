package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import me.tijlvdb.voltis.ui.animationsEnabled
import me.tijlvdb.voltis.ui.nav.HeroKey
import me.tijlvdb.voltis.ui.nav.heroElement
import me.tijlvdb.voltis.ui.rememberCoverRequest
import me.tijlvdb.voltis.ui.theme.VoltisShapes

/**
 * A 2:3 cover, with a placeholder when [url] is null or fails to load; offline, the cover stored
 * with a download in its place. Decorative: what it belongs to names it. [overlay] is drawn over the image (badges, a progress bar).
 * With a [hero] key the image is a shared element once it has loaded, never the placeholder.
 */
@Composable
fun VCover(url: String?, modifier: Modifier = Modifier, hero: HeroKey? = null, overlay: @Composable BoxScope.() -> Unit = {}) {
    val colors = MaterialTheme.colorScheme
    // Coil's crossfade doesn't follow the system animator scale by itself.
    val request = rememberCoverRequest(url, crossfade = animationsEnabled())
    // Per request: a stored cover arriving offline replaces a URL that failed.
    var failed by remember(request) { mutableStateOf(false) }
    var loaded by remember(request) { mutableStateOf(false) }
    // The backdrop goes once the image is up: while the image flies it mustn't leave a box behind.
    Box(modifier.aspectRatio(2f / 3f).clip(VoltisShapes.cover).then(if (loaded) Modifier else Modifier.background(colors.surfaceContainerHigh))) {
        // Clipped inside the shared element too, since it is drawn on its own while it flies.
        val flying = if (loaded) Modifier.heroElement(hero).fillMaxSize().clip(VoltisShapes.cover) else Modifier.fillMaxSize()
        if (request == null || failed) {
            Icon(
                if (failed) VIcons.ImageOff else VIcons.BookOpen,
                contentDescription = null,
                Modifier.align(Alignment.Center).size(40.dp),
                tint = colors.onSurfaceVariant,
            )
        } else {
            AsyncImage(
                model = request,
                contentDescription = null,
                modifier = flying,
                contentScale = ContentScale.Crop,
                onSuccess = { loaded = true },
                onError = { failed = true },
            )
        }
        overlay()
    }
}
