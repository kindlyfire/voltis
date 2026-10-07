package me.tijlvdb.voltis.ui.downloads

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.api.attemptResult
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.data.content.ContentRepository
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.domain.catalog.ItemLabels
import me.tijlvdb.voltis.domain.catalog.MAX_BULK_DOWNLOADS
import me.tijlvdb.voltis.domain.catalog.itemName
import me.tijlvdb.voltis.domain.catalog.splitItemTitle
import me.tijlvdb.voltis.domain.downloads.SeriesPolicy
import me.tijlvdb.voltis.ui.LoadStatus
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.itemLabels
import me.tijlvdb.voltis.ui.kit.AnchorFocusReturn
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.PopoverAnchor
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VChip
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VSheet

@HiltViewModel
class DownloadSheetViewModel internal constructor(
    private val list: suspend (seriesId: String) -> List<Content>,
    private val seriesRows: (seriesId: String) -> Flow<List<DownloadEntity>>,
    private val enqueue: suspend (List<Content>) -> Unit,
    private val policies: Flow<Map<String, SeriesPolicy>> = flowOf(emptyMap()),
    /** The id of the volume Continue opens, null when there is none. */
    private val continueOf: suspend (seriesId: String) -> String? = { null },
    private val saved: SavedStateHandle = SavedStateHandle(),
) : ViewModel() {
    @Inject
    constructor(content: ContentRepository, downloads: DownloadRepository, saved: SavedStateHandle) :
        this(content::volumes, downloads::series, { downloads.enqueue(it) }, downloads.policies, { content.continueTarget(it).target?.id }, saved)

    /** The series' automatic downloads, null when off. */
    fun policy(seriesId: String): Flow<SeriesPolicy?> = policies.map { it[seriesId] }

    /** The series' comic volumes in order, as of the sheet's last opening; null while they first load. */
    var volumes by mutableStateOf<List<Content>?>(null)
        private set

    var error by mutableStateOf<UiText?>(null)
        private set

    /** Where reading is in [volumes]: the "Next" shortcut starts here. */
    var current by mutableStateOf<Int?>(null)
        private set

    /** The shortcut the user last tapped, null once the range is edited by hand. */
    var chosen by mutableStateOf<Shortcut?>(null)
        private set

    /** The range to download, as positions in [volumes]. */
    var range by mutableStateOf(RangeSel())
        private set

    private var loading: Job? = null

    /** The `keep` of the last [load], which [retry] repeats. */
    private var keeping = false

    fun retry(seriesId: String) = load(seriesId, keeping)

    fun rows(seriesId: String) = seriesRows(seriesId)

    /**
     * Each opening reads the list again. The range starts at the current volume and runs to the last. With
     * [keep], set when the same dialog is recreated (a rotation, or the process dying behind it), the range
     * the user already made for this series stays: it is kept as volume ids in [saved] and checked against
     * the fresh list, and a cleared range stays cleared. A fresh opening, or a range whose ends are gone or
     * out of order, uses the defaults.
     */
    fun load(seriesId: String, keep: Boolean) {
        loading?.cancel()
        error = null
        keeping = keep
        // Until this opening has loaded and reconciled its range, there is nothing to edit or confirm.
        // The saved ids stay for a recreation; a fresh opening drops the last one's.
        volumes = null
        chosen = null
        if (!keep) remember(null)
        loading = viewModelScope.launch {
            attemptResult { list(seriesId).filter { it.type == ContentType.COMIC } }
                .onSuccess { fresh ->
                    val at = currentIndex(fresh, attemptResult { continueOf(seriesId) }.getOrNull())
                    current = at
                    val kept = if (keep) restored(seriesId, fresh) else null
                    range = kept ?: defaultRange(fresh, at)
                    chosen = if (kept != null) saved.get<String>(SHORTCUT)?.let { name -> Shortcut.entries.firstOrNull { it.name == name } } else null
                    volumes = fresh
                    remember(seriesId)
                }
                .onFailure { error = it.toUiText() }
        }
    }

    private fun restored(seriesId: String, fresh: List<Content>): RangeSel? {
        if (saved.get<String>(SERIES) != seriesId) return null
        val from = saved.get<String>(FROM) ?: return if (saved.get<Boolean>(CLEARED) == true) RangeSel() else null
        val ids = fresh.map { it.id }
        val start = ids.indexOf(from)
        val end = ids.indexOf(saved.get<String>(TO))
        // Equal ends are a one-volume range.
        return if (start >= 0 && end >= start) RangeSel(start, end) else null
    }

    /** Keeps [range] in [saved], by volume id. */
    private fun remember(seriesId: String?) {
        val list = volumes.orEmpty()
        saved[SERIES] = seriesId
        saved[FROM] = range.from?.let { list.getOrNull(it)?.id }
        saved[TO] = range.to?.let { list.getOrNull(it)?.id }
        saved[SHORTCUT] = chosen?.name
        saved[CLEARED] = seriesId != null && range.from == null
    }

    fun pickFrom(index: Int) {
        if (volumes == null) return
        range = range.withFrom(index, volumes.orEmpty())
        chosen = null
        remember(saved.get<String>(SERIES))
    }

    fun pickTo(index: Int) {
        if (volumes == null) return
        range = range.withTo(index)
        chosen = null
        remember(saved.get<String>(SERIES))
    }

    fun shortcut(shortcut: Shortcut) {
        if (volumes == null) return
        shortcutRange(shortcut, volumes.orEmpty(), current)?.let {
            range = it
            chosen = shortcut
            remember(saved.get<String>(SERIES))
        }
    }

    fun clear() {
        if (volumes == null) return
        range = RangeSel()
        chosen = null
        remember(saved.get<String>(SERIES))
    }

    /** Queues [items], the volumes of the range without a download, in the series' order; [onError] gets why it couldn't. */
    fun download(items: List<Content>, onError: (UiText) -> Unit) {
        if (volumes == null) return
        // The next opening starts from the defaults.
        range = RangeSel()
        chosen = null
        remember(null)
        viewModelScope.launch { attempt { enqueue(items) }?.let(onError) }
    }
}

/**
 * The volumes of [series] to download (P2 §11), as a range: From and To pickers over the volume
 * names, shortcuts that fill them, and a summary of what the range holds. The Download button is
 * pinned under the content. Volumes already downloaded or queued are left out and counted.
 * [askNotifications] belongs to the caller, since the sheet closes as it asks.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun DownloadSheet(series: Content, askNotifications: () -> Unit, onDismiss: () -> Unit, anchor: PopoverAnchor? = null, vm: DownloadSheetViewModel = hiltViewModel()) {
    // Outside the sheet's own window, the anchor is in the page's.
    AnchorFocusReturn(anchor)
    // Set once this dialog has loaded, so a recreation of it keeps the range but a new opening does not.
    var opened by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(series.id) {
        vm.load(series.id, keep = opened)
        opened = true
    }
    val rows by remember(series.id) { vm.rows(series.id) }.collectAsStateWithLifecycle(emptyList())
    val policy by remember(series.id) { vm.policy(series.id) }.collectAsStateWithLifecycle(null)
    var autoSheet by rememberSaveable { mutableStateOf(false) }
    var picking by rememberSaveable { mutableStateOf<String?>(null) }
    val context = LocalContext.current
    val snackbars = LocalSnackbars.current
    val volumes = vm.volumes
    val labels = itemLabels()
    val plan = volumes?.let { rangePlan(it, vm.range, rows.associate { row -> row.contentId to row.state }) }
    VSheet(
        stringResource(R.string.downloads_download),
        onDismiss,
        footer = {
            VButton(
                if (plan != null && plan.count > 0) stringResource(R.string.downloads_download_count, plan.count) else stringResource(R.string.downloads_download),
                {
                    askNotifications()
                    vm.download(plan?.items.orEmpty()) { snackbars.show(it.string(context)) }
                    onDismiss()
                },
                Modifier.fillMaxWidth(),
                enabled = plan != null && plan.count > 0 && !plan.tooMany,
            )
        },
    ) {
        if (volumes == null) {
            // About the height of the two fields, the chips and the summary, so the sheet doesn't grow when they arrive.
            Box(Modifier.fillMaxWidth().heightIn(min = LOADED_HEIGHT), contentAlignment = Alignment.Center) { LoadStatus(true, vm.error) { vm.retry(series.id) } }
        } else {
            val name = { i: Int -> itemName(volumes[i], series, labels) }
            val from = vm.range.from
            val to = vm.range.to
            Column(Modifier.fillMaxWidth(), Arrangement.spacedBy(8.dp)) {
                RangeField(stringResource(R.string.downloads_range_from), from?.let(name), { picking = FROM }, enabled = volumes.isNotEmpty())
                RangeField(stringResource(R.string.downloads_range_to), to?.let(name), { picking = TO }, enabled = toOptions(volumes, from).isEmpty().not())
            }
            val chips = listOf(
                Shortcut.All to stringResource(R.string.downloads_range_all),
                Shortcut.Unread to stringResource(R.string.downloads_range_unread),
                Shortcut.Next to stringResource(R.string.downloads_range_next, NEXT_COUNT),
            )
            FlowRow(Modifier.fillMaxWidth().padding(top = 4.dp), Arrangement.spacedBy(8.dp), Arrangement.Center) {
                for ((shortcut, text) in chips) {
                    val fills = shortcutRange(shortcut, volumes, vm.current)
                    VChip(text, shortcut == vm.chosen, { vm.shortcut(shortcut) }, enabled = fills != null)
                }
                VButton(stringResource(R.string.downloads_range_clear), vm::clear, style = VButtonStyle.Text, enabled = from != null)
            }
            RangeSummary(plan!!, from?.let { rangeLabel(volumes[it], series, labels) }, to?.let { rangeLabel(volumes[it], series, labels) })
        }
        AutoRow(policy, { autoSheet = true })
        if (autoSheet) AutoDownloadSheet(series.id, onDismiss = { autoSheet = false })
    }
    if (volumes != null && picking != null) {
        val from = vm.range.from
        val isFrom = picking == FROM
        val options = (if (isFrom) volumes.indices else toOptions(volumes, from)).map { it to itemName(volumes[it], series, labels) }
        ChapterPicker(
            stringResource(if (isFrom) R.string.downloads_range_from else R.string.downloads_range_to),
            options,
            if (isFrom) from else vm.range.to,
            { if (isFrom) vm.pickFrom(it) else vm.pickTo(it) },
            onDismiss = { picking = null },
        )
    }
}

private val LOADED_HEIGHT = 270.dp
private const val FROM = "from"
private const val TO = "to"
private const val SERIES = "series"
private const val SHORTCUT = "shortcut"
private const val CLEARED = "cleared"

/** "Chapter 5", the volume's number alone when it has one. */
private fun rangeLabel(volume: Content, series: Content, labels: ItemLabels) = splitItemTitle(volume, series, labels).label ?: itemName(volume, series, labels)

/** The count and size of the range, and what it leaves out; read out when it changes. */
@Composable
private fun RangeSummary(plan: RangePlan, from: String?, to: String?) {
    val colors = MaterialTheme.colorScheme
    val size = fileSize(plan.bytes).let { if (plan.sizeUnknown && plan.count > 0) stringResource(R.string.downloads_range_at_least, it) else it }
    Column(
        Modifier.fillMaxWidth().padding(top = 4.dp).background(colors.surfaceContainerHighest, MaterialTheme.shapes.medium).padding(horizontal = 16.dp, vertical = 12.dp)
            .semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite },
        Arrangement.spacedBy(2.dp),
    ) {
        if (from == null || to == null) {
            Text(stringResource(R.string.downloads_range_none), color = colors.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
            return@Column
        }
        Text(if (from == to) from else stringResource(R.string.downloads_range_title, from, to), style = MaterialTheme.typography.titleMedium)
        Text(
            if (plan.count > 0) stringResource(R.string.downloads_range_count, plan.count, size) else stringResource(R.string.downloads_range_nothing),
            style = MaterialTheme.typography.bodyMedium,
        )
        if (plan.present > 0) {
            Text(stringResource(R.string.downloads_range_skipped, plan.present), color = colors.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
        }
        if (plan.stuck > 0) {
            Text(stringResource(R.string.downloads_range_stuck, plan.stuck), color = colors.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
        }
        if (plan.tooMany) {
            Text(stringResource(R.string.downloads_range_too_many, MAX_BULK_DOWNLOADS), color = colors.error, style = MaterialTheme.typography.bodyMedium)
        }
    }
}

/** "Auto-download", with "Off" or "Next 3": it opens [AutoDownloadSheet]. */
@Composable
private fun AutoRow(policy: SeriesPolicy?, onClick: () -> Unit) {
    val value = when {
        policy == null -> stringResource(R.string.downloads_auto_off)
        policy.keepNext > 0 -> stringResource(R.string.downloads_auto_next, policy.keepNext)
        else -> stringResource(R.string.downloads_auto_delete)
    }
    Row(
        Modifier.fillMaxWidth().padding(top = 4.dp).clickable(role = Role.Button, onClick = onClick).heightIn(min = 48.dp),
        Arrangement.spacedBy(16.dp),
        Alignment.CenterVertically,
    ) {
        Text(stringResource(R.string.downloads_auto), Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
        Text(value, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
        Icon(VIcons.ChevronRight, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}
