package me.tijlvdb.voltis.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import coil3.request.ImageRequest
import kotlin.math.roundToInt
import coil3.size.Precision
import coil3.size.Size
import me.tijlvdb.voltis.domain.catalog.MAX_ITEM_SIZE
import coil3.request.crossfade
import java.io.File
import me.tijlvdb.voltis.data.downloads.forUrl

/** What a cover is decoded to, wherever it is shown: the widest consumer (the header's 240 dp cover, or the largest card) at this screen's density. */
private fun coverSize(density: Float): Size {
    val width = maxOf(240, MAX_ITEM_SIZE)
    return Size((width * density).roundToInt(), (width * 1.5f * density).roundToInt())
}

/** The page's backdrop: a thumbnail the filtering of the upscale turns into a blur. */
val BackdropSize = Size(24, 36)

/** The covers stored with downloads, while offline (`StoredCovers.offline`). */
val LocalStoredCovers = compositionLocalOf<Map<String, File>> { emptyMap() }

/**
 * What Coil loads for a cover URL: offline, the cover stored in a download's copy (a path no other
 * copy uses); else the URL. A new request whenever that changes, so a failed one is tried again.
 */
@Composable
fun rememberCoverRequest(url: String?, crossfade: Boolean = false, sized: Boolean = true, backdrop: Boolean = false): ImageRequest? {
    val context = LocalContext.current
    val density = LocalDensity.current.density
    val size = if (backdrop) BackdropSize else if (sized) coverSize(density) else null
    val stored = LocalStoredCovers.current.forUrl(url)
    return remember(url, stored, crossfade, size) {
        when {
            stored != null -> ImageRequest.Builder(context).data(stored)
            url != null -> ImageRequest.Builder(context).data(url)
            else -> return@remember null
        }.apply {
            // One size for every card, row and header: a cover decoded for one is valid in the memory cache for all
            // of them, so it shows at once and without the crossfade (which Coil skips for a memory-cache hit).
            if (size != null) size(size).precision(if (backdrop) Precision.EXACT else Precision.INEXACT)
        }.crossfade(crossfade).build()
    }
}
