package me.tijlvdb.voltis.ui.downloads

import android.text.format.Formatter
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.domain.downloads.DownloadError
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.Stale

@Composable
fun fileSize(bytes: Long): String = Formatter.formatShortFileSize(LocalContext.current, bytes)

/** An app error's sentence; the server's own words are never shown, only what they come to. */
@Composable
fun downloadError(error: String?): String = when (error) {
    DownloadError.UNREACHABLE -> stringResource(R.string.downloads_error_unreachable)
    DownloadError.BROKEN_OFF -> stringResource(R.string.downloads_error_broken_off)
    DownloadError.NO_SPACE -> stringResource(R.string.downloads_error_no_space)
    DownloadError.WRITE_FAILED -> stringResource(R.string.downloads_error_write_failed)
    DownloadError.GONE -> stringResource(R.string.downloads_error_gone)
    DownloadError.FILE_CHANGED -> stringResource(R.string.downloads_error_file_changed)
    DownloadError.MISSING_FILES -> stringResource(R.string.downloads_error_missing_files)
    DownloadError.PROTOCOL -> stringResource(R.string.downloads_error_protocol)
    DownloadError.UNEXPECTED -> stringResource(R.string.downloads_error_unexpected)
    null, "" -> stringResource(R.string.downloads_error_failed)
    NO_PAGES -> stringResource(R.string.downloads_error_no_pages)
    else -> stringResource(R.string.downloads_error_server)
}

/** The server's message for a comic without pages, which no retry changes. */
private const val NO_PAGES = "Content has no pages"

/** One line for a row that isn't done: its progress, or why it waits or failed. */
@Composable
fun downloadStatus(row: DownloadEntity): String = when (row.state) {
    DownloadState.RUNNING -> row.pageCount?.let { stringResource(R.string.downloads_page_of, row.pagesDone, it) }
        ?: stringResource(R.string.downloads_starting)
    DownloadState.PAUSED -> stringResource(R.string.downloads_paused)
    DownloadState.FAILED -> downloadError(row.error)
    // A queued row keeps the error that sent it back, until it runs again.
    else -> if (row.error == DownloadError.UNREACHABLE) downloadError(row.error) else stringResource(R.string.downloads_queued)
}

/** Why a done download no longer matches the server, or null. */
@Composable
fun staleLabel(stale: String?): String? = when (stale) {
    Stale.VERSION -> stringResource(R.string.downloads_stale_version)
    Stale.DAMAGED -> stringResource(R.string.downloads_damaged)
    Stale.GONE -> stringResource(R.string.downloads_error_gone)
    else -> null
}

/** Retry applies: failed, or waiting for the network; not when the item is gone from the server. */
val DownloadEntity.retryable get() = error != DownloadError.GONE && (state == DownloadState.FAILED || error == DownloadError.UNREACHABLE)

/** "Download again" for a copy whose files went missing, which downloads it anew; else Retry. */
@Composable
fun retryLabel(row: DownloadEntity): String =
    stringResource(if (row.error == DownloadError.MISSING_FILES) R.string.downloads_download_again else R.string.retry)

/** The fraction of pages stored, 0 before the manifest. */
val DownloadEntity.fraction get() = pageCount?.takeIf { it > 0 }?.let { pagesDone.toFloat() / it } ?: 0f
