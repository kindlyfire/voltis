package me.tijlvdb.voltis.ui.downloads

import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.res.stringResource
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.domain.downloads.DownloadBadge
import me.tijlvdb.voltis.ui.kit.VBadge
import me.tijlvdb.voltis.ui.kit.VIcons

/** A card's download mark over its cover. Decorative: the card's description says it ([downloadBadgeLabel]). */
@Composable
fun DownloadBadgeIcon(badge: DownloadBadge, modifier: Modifier = Modifier) = VBadge(badge.icon(), modifier)

@Composable
fun downloadBadgeLabel(badge: DownloadBadge): String = stringResource(
    when (badge) {
        DownloadBadge.QUEUED -> R.string.badge_queued
        DownloadBadge.DOWNLOADING -> R.string.badge_downloading
        DownloadBadge.DOWNLOADED -> R.string.badge_downloaded
        DownloadBadge.FAILED -> R.string.badge_failed
        DownloadBadge.UPDATED -> R.string.badge_stale
        DownloadBadge.DAMAGED -> R.string.downloads_damaged
        DownloadBadge.GONE -> R.string.downloads_error_gone
    },
)

@Composable
private fun DownloadBadge.icon(): Painter = when (this) {
    DownloadBadge.QUEUED -> VIcons.Queued
    DownloadBadge.DOWNLOADING -> VIcons.Download
    DownloadBadge.DOWNLOADED -> VIcons.DownloadDone
    DownloadBadge.FAILED -> VIcons.AlertCircle
    DownloadBadge.UPDATED -> VIcons.Updated
    DownloadBadge.DAMAGED -> VIcons.AlertCircle
    DownloadBadge.GONE -> VIcons.CloudOff
}
