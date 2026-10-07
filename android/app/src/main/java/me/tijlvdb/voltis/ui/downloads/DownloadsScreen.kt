package me.tijlvdb.voltis.ui.downloads

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.platform.LocalContext
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.listSaver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.ProgressBarRangeInfo
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.progressBarRangeInfo
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.delay
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.content.coverUrl
import me.tijlvdb.voltis.data.db.DownloadRow
import me.tijlvdb.voltis.domain.downloads.DownloadError
import me.tijlvdb.voltis.domain.downloads.DownloadState
import me.tijlvdb.voltis.domain.downloads.ErrorKind
import me.tijlvdb.voltis.domain.downloads.Stale
import me.tijlvdb.voltis.domain.downloads.offersAgain
import me.tijlvdb.voltis.ui.OfflineBar
import me.tijlvdb.voltis.ui.needsConnection
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.bottomSpace
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.PageHeader
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VCover
import me.tijlvdb.voltis.ui.kit.VDialog
import me.tijlvdb.voltis.ui.kit.VDisabled
import me.tijlvdb.voltis.ui.kit.VIconButton
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VOverflowMenu
import me.tijlvdb.voltis.ui.kit.VProgressBar
import me.tijlvdb.voltis.ui.kit.VSpinner
import me.tijlvdb.voltis.ui.kit.VTopBar
import me.tijlvdb.voltis.ui.pageGutter
import me.tijlvdb.voltis.ui.pageGutterBeforeIcon
import me.tijlvdb.voltis.ui.readableWidth
import me.tijlvdb.voltis.ui.theme.VoltisShapes
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/** What a delete confirmation is about: [ids] (null for all), named by [title] for [count] items. */
private data class PendingDelete(val ids: List<String>?, val title: String?, val count: Int)

/** The queue and the downloaded items, grouped by series (P2 §11); [seriesId] opens with that series' volumes shown; a [landing] has no Back arrow. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DownloadsScreen(
    onBack: () -> Unit,
    openContent: (String) -> Unit,
    openReader: (String) -> Unit,
    openAttention: () -> Unit,
    seriesId: String? = null,
    landing: Boolean = false,
    vm: DownloadsViewModel = hiltViewModel(),
) {
    val content by vm.content.collectAsStateWithLifecycle()
    val list = content?.list
    val failed by vm.failed.collectAsStateWithLifecycle()
    val online by vm.online.collectAsStateWithLifecycle()
    val lifecycle = LocalLifecycleOwner.current
    val context = LocalContext.current
    val snackbars = LocalSnackbars.current
    LaunchedEffect(vm) { vm.messages.collect { snackbars.show(it.string(context)) } }
    LaunchedEffect(vm) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) {
            while (true) {
                vm.check()
                delay(5_000)
            }
        }
    }
    val expanded = rememberSaveable(saver = listSaver({ it.value.toList() }, { mutableStateOf(it.toSet()) })) { mutableStateOf(setOfNotNull(seriesId)) }
    var pending by remember { mutableStateOf<PendingDelete?>(null) }
    val state = rememberLazyListState()
    // Once, to the series asked for: past the header, the queue and the heading, and the groups before it (closed then).
    var scrolledTo by rememberSaveable { mutableStateOf(seriesId == null) }
    // The attention entry comes first, and is in the content.
    LaunchedEffect(content) {
        val current = content?.list ?: return@LaunchedEffect
        val entries = content?.attention ?: return@LaunchedEffect
        if (scrolledTo) return@LaunchedEffect
        scrolledTo = true
        val group = current.groups.indexOfFirst { it.seriesId == seriesId }.takeIf { it >= 0 } ?: return@LaunchedEffect
        val queue = if (current.queue.isEmpty()) 0 else current.queue.size + 1
        state.scrollToItem(2 + queue + group + if (entries > 0) 1 else 0)
    }
    val scrolled by remember { derivedStateOf { state.firstVisibleItemIndex > 0 } }
    val title = stringResource(R.string.downloads_title)
    Surface(Modifier.fillMaxSize()) {
        Column {
            VTopBar(if (scrolled) title else "", onBack, back = !landing)
            PullToRefreshBox(vm.refreshing, vm::refresh) {
                LazyColumn(Modifier.fillMaxSize(), state, PaddingValues(bottom = bottomSpace())) {
                    item(key = "header", contentType = "header") {
                        // The end pads as the rows below do, so the menu's glyph lines up with their buttons'.
                        Column(Modifier.readableWidth(pageGutter()).padding(start = pageGutter(), end = pageGutterBeforeIcon()), Arrangement.spacedBy(8.dp)) {
                            // By the title: at the bar's end it would be far from the column on a wide window.
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                PageHeader(title, Modifier.weight(1f))
                                // The slot is always there: the menu arriving with the content would grow the row.
                                Box(Modifier.padding(top = 8.dp).size(48.dp)) {
                                    if (list?.let { it.queue.isNotEmpty() || it.groups.isNotEmpty() } == true) {
                                        VOverflowMenu(listOf(stringResource(R.string.downloads_delete_all) to { pending = PendingDelete(null, null, 0) }))
                                    }
                                }
                            }
                            // Without the database the line would count nothing.
                            content?.takeIf { failed == null }?.let {
                                Text(
                                    stringResource(R.string.downloads_storage, fileSize(it.list.bytes), fileSize(it.free)),
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    style = MaterialTheme.typography.bodyMedium,
                                )
                            }
                            if (failed != null) QueryError(UiText.Res(R.string.error_offline_data), retry = vm::retryOpen)
                            OfflineBar()
                            content?.takeIf { failed == null }?.let { SyncLine(it.unsent, vm) }
                        }
                    }
                    // First: what wasn't synced matters most.
                    content?.attention?.takeIf { it > 0 }?.let { count -> item(key = "attention", contentType = "attention") { AttentionEntry(count, openAttention) } }
                    val current = list
                    val wifiOnly = content?.wifiOnly ?: true
                    when {
                        // Nothing is known without the database: no list, no empty state.
                        failed != null -> Unit
                        current == null -> item(key = "loading") { Column(Modifier.readableWidth(pageGutter()).padding(horizontal = pageGutter(), vertical = 16.dp)) { VSpinner() } }
                        current.queue.isEmpty() && current.groups.isEmpty() -> item(key = "empty") {
                            Text(
                                stringResource(R.string.downloads_empty),
                                Modifier.readableWidth(pageGutter()).padding(horizontal = pageGutter(), vertical = 16.dp),
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        else -> {
                            if (current.queue.isNotEmpty()) {
                                item(key = "queue", contentType = "heading") {
                                    QueueHeading(current.queue, queueNote(current.queue, wifiOnly, vm.metered, online), vm)
                                }
                                items(current.queue, key = { "q:" + it.download.contentId }, contentType = { "queued" }) { QueueRow(it, vm) }
                            }
                            if (current.groups.isNotEmpty()) {
                                item(key = "done", contentType = "heading") { Heading(stringResource(R.string.downloads_done_heading)) }
                                for (group in current.groups) {
                                    item(key = "g:" + group.seriesId, contentType = "group") {
                                        GroupRow(group, group.seriesId in expanded.value, expanded, openContent, openReader, online, vm::again) {
                                            pending = PendingDelete(group.items.map { it.download.contentId }, group.title, group.items.size)
                                        }
                                    }
                                    if (!group.standalone && group.seriesId in expanded.value) {
                                        items(group.items, key = { "v:" + it.download.contentId }, contentType = { "volume" }) { row ->
                                            VolumeRow(row, openReader, online, vm::again) {
                                                pending = PendingDelete(listOf(row.download.contentId), row.title ?: row.download.contentId, 1)
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
    pending?.let { request ->
        val (titleText, text) = when {
            request.ids == null -> stringResource(R.string.downloads_delete_all_confirm) to stringResource(R.string.downloads_delete_all_text)
            request.count == 1 -> stringResource(R.string.downloads_delete_one, request.title.orEmpty()) to stringResource(R.string.downloads_delete_text)
            else -> pluralStringResource(R.plurals.downloads_delete_series, request.count, request.count, request.title.orEmpty()) to
                stringResource(R.string.downloads_delete_text)
        }
        VDialog(
            titleText,
            stringResource(R.string.downloads_delete),
            onConfirm = {
                pending = null
                if (request.ids == null) vm.deleteAll() else vm.delete(request.ids)
            },
            onDismiss = { pending = null },
            text = text,
            danger = true,
        )
    }
}

/** "Up to date", or "3 changes waiting to sync" with Sync now, which needs a connection. */
@Composable
private fun SyncLine(count: Int, vm: DownloadsViewModel) {
    Row(Modifier.heightIn(min = 48.dp), Arrangement.spacedBy(8.dp), Alignment.CenterVertically) {
        Text(
            if (count == 0) stringResource(R.string.sync_up_to_date) else pluralStringResource(R.plurals.sync_waiting, count, count),
            Modifier.weight(1f).semantics { liveRegion = LiveRegionMode.Polite },
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodyMedium,
        )
        if (count > 0) {
            val label = stringResource(R.string.sync_now)
            VDisabled(needsConnection(), label) { enabled -> VButton(label, vm::syncNow, style = VButtonStyle.Text, enabled = enabled && !vm.syncing) }
        }
    }
}

/** "2 items need attention", opening the list. */
@Composable
private fun AttentionEntry(count: Int, onClick: () -> Unit) {
    val colors = VoltisTheme.colors
    Row(
        Modifier.readableWidth(pageGutter())
            .padding(horizontal = pageGutter())
            .padding(top = 16.dp)
            .clip(VoltisShapes.toast)
            .background(colors.infoContainer)
            .clickable(role = Role.Button, onClick = onClick)
            .heightIn(min = 56.dp)
            .padding(horizontal = 16.dp),
        Arrangement.spacedBy(12.dp),
        Alignment.CenterVertically,
    ) {
        Icon(VIcons.AlertCircle, contentDescription = null, tint = colors.onInfoContainer)
        Text(
            pluralStringResource(R.plurals.attention_items, count, count),
            Modifier.weight(1f).semantics { liveRegion = LiveRegionMode.Polite },
            color = colors.onInfoContainer,
            style = MaterialTheme.typography.bodyLarge,
        )
        Icon(VIcons.ChevronRight, contentDescription = null, tint = colors.onInfoContainer)
    }
}

@Composable
private fun Heading(text: String) {
    Text(
        text,
        Modifier.readableWidth(pageGutter()).padding(horizontal = pageGutter()).padding(top = 24.dp, bottom = 4.dp).semantics { heading() },
        style = MaterialTheme.typography.titleMedium,
    )
}

/** "Queue", Pause all or Resume all, and what it waits for. */
@Composable
private fun QueueHeading(queue: List<DownloadRow>, note: QueueNote?, vm: DownloadsViewModel) {
    val states = queue.map { it.download.state }
    Column(Modifier.readableWidth(pageGutter()).padding(horizontal = pageGutter()).padding(top = 16.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.downloads_queue), Modifier.weight(1f).semantics { heading() }, style = MaterialTheme.typography.titleMedium)
            if (DownloadState.QUEUED in states || DownloadState.RUNNING in states) {
                VButton(stringResource(R.string.downloads_pause_all), vm::pauseAll, style = VButtonStyle.Text)
            }
            val storage = queue.any { it.download.state == DownloadState.FAILED && it.download.errorKind == ErrorKind.STORAGE }
            if (DownloadState.PAUSED in states || storage) VButton(stringResource(R.string.downloads_resume_all), vm::resumeAll, style = VButtonStyle.Text)
        }
        // Always there, so a change is announced.
        Text(
            when (note) {
                QueueNote.STORAGE -> stringResource(R.string.downloads_paused_storage)
                QueueNote.WIFI -> stringResource(R.string.downloads_waiting_wifi)
                QueueNote.CONNECTION -> stringResource(R.string.downloads_error_unreachable)
                null -> ""
            },
            Modifier.semantics { liveRegion = LiveRegionMode.Polite },
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodyMedium,
        )
    }
}

/** [plain] when the item has no [title], else [named] with it: never the raw content id. */
@Composable
private fun named(plain: Int, named: Int, title: String?): String = if (title == null) stringResource(plain) else stringResource(named, title)

/** An item not done yet: its progress or state, with the controls that apply. */
@Composable
private fun QueueRow(row: DownloadRow, vm: DownloadsViewModel) {
    val download = row.download
    val id = download.contentId
    // Each queue row has its own buttons: they name the item, or a list of them reads "Cancel, Cancel".
    val name = row.title
    Row(
        Modifier.readableWidth(pageGutter()).padding(start = pageGutter(), end = pageGutterBeforeIcon()).heightIn(min = 64.dp),
        Arrangement.spacedBy(4.dp),
        Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f).padding(vertical = 8.dp).semantics(mergeDescendants = true) {}, Arrangement.spacedBy(2.dp)) {
            row.seriesTitle?.let {
                Text(it, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.labelMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            Text(row.title ?: id, style = MaterialTheme.typography.bodyLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
            if (download.state == DownloadState.RUNNING) {
                VProgressBar(
                    download.fraction,
                    Modifier.fillMaxWidth().padding(vertical = 4.dp).semantics { progressBarRangeInfo = ProgressBarRangeInfo(download.fraction, 0f..1f) },
                )
            }
            Text(
                downloadStatus(download),
                color = if (download.state == DownloadState.FAILED) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodySmall,
            )
        }
        when (download.state) {
            DownloadState.RUNNING -> VIconButton(VIcons.PauseFilled, named(R.string.downloads_pause, R.string.downloads_pause_named, name), { vm.pause(id) })
            DownloadState.PAUSED -> VIconButton(VIcons.PlayFilled, named(R.string.downloads_resume, R.string.downloads_resume_named, name), { vm.resume(id) })
            DownloadState.FAILED, DownloadState.QUEUED -> if (download.retryable) VIconButton(VIcons.Refresh, (if (download.error == DownloadError.MISSING_FILES) named(R.string.downloads_download_again, R.string.downloads_download_again_named, name) else named(R.string.retry, R.string.downloads_retry_named, name)), { vm.retry(id) })
        }
        VIconButton(VIcons.Close, named(R.string.cancel, R.string.downloads_cancel_named, name), { vm.cancel(id) })
    }
}

/** A series (opening its page, with its volumes behind the chevron) or a standalone item (opening the reader). */
@Composable
private fun GroupRow(
    group: DownloadGroup,
    open: Boolean,
    expanded: MutableState<Set<String>>,
    openContent: (String) -> Unit,
    openReader: (String) -> Unit,
    online: Boolean,
    onAgain: (String) -> Unit,
    onDelete: () -> Unit,
) {
    // A standalone item is its only row: its stale mark, and Download again, are the group's.
    val only = group.items.singleOrNull()?.download?.takeIf { group.standalone }
    val stale = only?.stale
    Row(
        Modifier.readableWidth(pageGutter())
            .clickable(onClickLabel = stringResource(R.string.downloads_open_series, group.title)) {
                if (group.standalone) openReader(group.seriesId) else openContent(group.seriesId)
            }
            .padding(start = pageGutter(), end = pageGutterBeforeIcon(), top = 8.dp, bottom = 8.dp),
        Arrangement.spacedBy(12.dp),
        Alignment.CenterVertically,
    ) {
        VCover(coverUrl(group.seriesId, group.coverVersion), Modifier.width(48.dp))
        Column(Modifier.weight(1f).semantics(mergeDescendants = true) {}) {
            Text(group.title, style = MaterialTheme.typography.bodyLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text(
                (if (group.standalone) fileSize(group.bytes) else pluralStringResource(R.plurals.downloads_volumes, group.items.size, group.items.size, fileSize(group.bytes))) +
                    if (group.auto) " · " + stringResource(R.string.downloads_auto_mark) else "",
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodySmall,
            )
            StaleLine(stale)
        }
        if (only != null && only.state == DownloadState.DONE && stale.offersAgain() && online) {
            VIconButton(VIcons.Download, stringResource(R.string.downloads_download_again_named, group.title), { onAgain(group.seriesId) })
        }
        if (!group.standalone) {
            VIconButton(
                if (open) VIcons.ChevronUp else VIcons.ChevronDown,
                stringResource(if (open) R.string.downloads_collapse else R.string.downloads_expand, group.title),
                { expanded.value = if (open) expanded.value - group.seriesId else expanded.value + group.seriesId },
            )
        }
        VIconButton(VIcons.Delete, stringResource(R.string.downloads_delete_named, group.title), onDelete)
    }
}

/** A downloaded volume of an expanded series: opens the reader. A new version on the server offers "Download again", online. */
@Composable
private fun VolumeRow(row: DownloadRow, openReader: (String) -> Unit, online: Boolean, onAgain: (String) -> Unit, onDelete: () -> Unit) {
    val id = row.download.contentId
    Row(
        Modifier.readableWidth(pageGutter()).clickable { openReader(id) }.padding(start = pageGutter() + 60.dp, end = pageGutterBeforeIcon()).heightIn(min = 56.dp),
        Arrangement.spacedBy(12.dp),
        Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f).semantics(mergeDescendants = true) {}) {
            Text(row.title ?: id, style = MaterialTheme.typography.bodyLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text(fileSize(row.download.copyBytes), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
            StaleLine(row.download.stale)
        }
        if (row.download.state == DownloadState.DONE && row.download.stale.offersAgain() && online) VIconButton(VIcons.Download, named(R.string.downloads_download_again, R.string.downloads_download_again_named, row.title), { onAgain(id) })
        VIconButton(VIcons.Delete, named(R.string.downloads_delete, R.string.downloads_delete_named, row.title), onDelete)
    }
}

/** Why a download no longer matches the server, under its size. */
@Composable
private fun StaleLine(stale: String?) {
    val label = staleLabel(stale) ?: return
    Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(when (stale) { Stale.GONE -> VIcons.CloudOff; Stale.DAMAGED -> VIcons.AlertCircle; else -> VIcons.Updated }, contentDescription = null, Modifier.size(16.dp), tint = MaterialTheme.colorScheme.primary)
        Text(label, color = MaterialTheme.colorScheme.primary, style = MaterialTheme.typography.bodySmall)
    }
}
