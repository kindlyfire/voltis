package me.tijlvdb.voltis.ui.grid

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyGridItemSpanScope
import androidx.compose.foundation.lazy.grid.LazyGridScope
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.runtime.DisposableEffect
import me.tijlvdb.voltis.ui.LocalBottomInset
import me.tijlvdb.voltis.ui.LocalSnackbarLift
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.paging.LoadState
import androidx.paging.compose.LazyPagingItems
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import androidx.activity.compose.LocalActivity
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import me.tijlvdb.voltis.data.lists.entryItem
import me.tijlvdb.voltis.ui.EffectHost
import me.tijlvdb.voltis.ui.lists.LISTS_SHEET
import me.tijlvdb.voltis.ui.lists.ListsSheet
import me.tijlvdb.voltis.ui.lists.ListsSheetViewModel
import me.tijlvdb.voltis.ui.lists.SheetEffect
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.data.content.GridFilters
import me.tijlvdb.voltis.data.content.PagedContent
import me.tijlvdb.voltis.domain.catalog.gridColumns
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.LocalOffline
import me.tijlvdb.voltis.ui.bottomSpace
import me.tijlvdb.voltis.ui.downloads.rememberAskNotifications
import me.tijlvdb.voltis.ui.needsConnection
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.PopoverAnchor
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VIconButton
import me.tijlvdb.voltis.ui.kit.VIconToggle
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VSpinner
import me.tijlvdb.voltis.ui.kit.VStarToggle
import me.tijlvdb.voltis.ui.kit.popoverAnchor
import me.tijlvdb.voltis.ui.pageGutter

/** What a grid shows: the pages of a library or of All libraries, or a plain list (a series' contents). */
class GridItems(
    val count: Int,
    val get: (index: Int) -> Content?,
    /** Of a row, null for none: it keeps its place when rows before it are dropped. Not an Int, which the header's items use. */
    val key: ((index: Int) -> Any)? = null,
    /** The first load: nothing to show yet. */
    val loading: Boolean = false,
    /** Reloading what is shown. */
    val refreshing: Boolean = false,
    val error: UiText? = null,
    val appending: Boolean = false,
    val appendError: UiText? = null,
    /** Rows before the retained ones are loading again, or failed to. */
    val prepending: Boolean = false,
    val prependError: UiText? = null,
    val retry: () -> Unit,
) {
    /** A list loaded in one request; null while it loads. */
    constructor(list: List<Content>?, refreshing: Boolean, error: UiText?, retry: () -> Unit) : this(
        count = list?.size ?: 0,
        get = { list?.get(it) },
        loading = list == null && error == null,
        refreshing = refreshing,
        error = error,
        retry = retry,
    )
}

fun LazyPagingItems<PagedContent>.asGridItems(): GridItems {
    val refresh = loadState.refresh
    val append = loadState.append
    val prepend = loadState.prepend
    return GridItems(
        count = itemCount,
        get = { this[it]?.content },
        key = { peek(it)?.let { row -> "row:${row.offset}" } ?: "gap:$it" },
        loading = refresh is LoadState.Loading && itemCount == 0,
        refreshing = refresh is LoadState.Loading && itemCount > 0,
        error = (refresh as? LoadState.Error)?.error?.toUiText(),
        appending = append is LoadState.Loading,
        appendError = (append as? LoadState.Error)?.error?.toUiText(),
        prepending = prepend is LoadState.Loading,
        prependError = (prepend as? LoadState.Error)?.error?.toUiText(),
        retry = ::retry,
    )
}

internal val ColumnGap = 18.dp
internal val RowGap = 24.dp

private enum class GridSheet { Display, Filters }

/**
 * The port of `ContentGrid.vue`: [header] as full-span items, the toolbar, then the cards of
 * [items], all in one scrolling grid. The columns follow the grid's width and the stored item
 * size. A card of the continue sorts opens the reader. With [parent], the series whose contents
 * these are, every card does, and is named by its number in it. A filter change scrolls back
 * to the header item [filterTop], if the grid was scrolled past it.
 *
 * In select mode (P4 §6) a tap toggles a card, the selection's bar covers the grid's bottom, and
 * Back leaves the mode; the screen swaps its top bar for [SelectionTopBar].
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ContentGrid(
    vm: GridViewModel,
    items: GridItems,
    openContent: (contentId: String) -> Unit,
    openReader: (contentId: String) -> Unit,
    modifier: Modifier = Modifier,
    state: LazyGridState = rememberLazyGridState(),
    parent: Content? = null,
    onRefresh: () -> Unit = vm::refresh,
    filterTop: Int = 0,
    /** False: only the [header] is shown, without the toolbar and the cards (a series page on its Details tab). */
    showItems: Boolean = true,
    header: LazyGridScope.() -> Unit = {},
) {
    val options = vm.options.collectAsStateWithLifecycle().value ?: return
    val badges by vm.badges.collectAsStateWithLifecycle(emptyMap())
    val filters = vm.filters
    val snackbars = LocalSnackbars.current
    val notDownloaded = stringResource(R.string.reader_not_downloaded)
    var sheet by rememberSaveable { mutableStateOf<GridSheet?>(null) }
    val scope = rememberCoroutineScope()
    val toolbarShown by remember { derivedStateOf { state.layoutInfo.visibleItemsInfo.any { it.contentType == "toolbar" } } }
    // A new filter opens at the top, not mid-list. With the toolbar in view the first row is already
    // under it, and scrolling would move a popover anchored to it.
    val setFilters = { to: GridFilters ->
        vm.applyFilters(to)
        if (!toolbarShown && state.firstVisibleItemIndex >= filterTop) scope.launch { state.scrollToItem(filterTop) }
        Unit
    }
    val gutter = pageGutter()
    val selection = vm.selection
    val running by vm.running.collectAsStateWithLifecycle()
    val full = stringResource(R.string.select_full)
    val toggle = { item: Content, series: String? -> if (!vm.toggle(item, series)) snackbars.show(full) }
    val density = LocalDensity.current
    var barHeight by remember { mutableStateOf(0.dp) }
    // Snackbars rise above the selection bar, which must stay reachable.
    val lift = LocalSnackbarLift.current
    val inset = LocalBottomInset.current
    DisposableEffect(selection.active, barHeight) {
        lift.value = if (selection.active) (barHeight - inset).coerceAtLeast(0.dp) else 0.dp
        onDispose { lift.value = 0.dp }
    }
    BoxWithConstraints(modifier.fillMaxSize()) {
        val width = (maxWidth - gutter * 2).value
        val columns = gridColumns(width, options.itemSize)
        val fullSpan: LazyGridItemSpanScope.() -> GridItemSpan = { GridItemSpan(maxLineSpan) }
        PullToRefreshBox(items.refreshing, onRefresh) {
            LazyVerticalGrid(
                GridCells.Fixed(columns),
                Modifier.fillMaxSize(),
                state,
                PaddingValues(start = gutter, end = gutter, bottom = if (selection.active) 24.dp + barHeight else bottomSpace()),
                horizontalArrangement = Arrangement.spacedBy(ColumnGap),
            ) {
                header()
                if (!showItems) return@LazyVerticalGrid
                item(span = fullSpan, contentType = "toolbar") {
                    Toolbar(
                        filters,
                        vm.total,
                        sheet,
                        { sheet = it },
                        stored = vm.stored,
                        onStar = { setFilters(filters.copy(starred = it)) },
                        selecting = selection.active,
                        onSelecting = vm::selecting,
                    ) { open, anchor ->
                        when (open) {
                            GridSheet.Display -> DisplayOptionsSheet(options, width, vm::updateOptions, vm::resetOptions, { sheet = null }, anchor)
                            GridSheet.Filters -> FilterSheet(filters, vm.sorts, setFilters, { sheet = null }, anchor)
                        }
                    }
                }
                if (items.error != null) {
                    item(span = fullSpan, contentType = "error") { QueryError(items.error, Modifier.padding(bottom = RowGap), items.retry) }
                }
                // Rows scrolled away from are loaded again in front of the retained ones.
                if (items.count > 0) {
                    if (items.prependError != null) {
                        item(key = "prepend", span = fullSpan, contentType = "error") { QueryError(items.prependError, Modifier.padding(bottom = RowGap), items.retry) }
                    } else if (items.prepending) {
                        item(key = "prepend", span = fullSpan, contentType = "appending") {
                            Box(Modifier.fillMaxWidth().padding(8.dp), Alignment.Center) { VSpinner(Modifier.size(28.dp)) }
                        }
                    }
                }
                val card = Modifier.padding(bottom = RowGap)
                when {
                    items.loading -> items(columns * 2, contentType = { "skeleton" }) {
                        // One of them says it, so TalkBack reads "Loading" once.
                        val label = stringResource(R.string.loading)
                        ContentCardSkeleton(
                            if (it == 0) card.semantics { contentDescription = label } else card,
                            subtitle = filters.listsContinue,
                            title = !options.hideTitle,
                        )
                    }
                    items.count == 0 && items.error == null -> item(span = fullSpan, contentType = "empty") {
                        Text(
                            stringResource(if (filters.hasFilters || filters.starred) R.string.grid_no_match else R.string.grid_empty),
                            Modifier.fillMaxWidth().padding(vertical = 48.dp),
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            textAlign = TextAlign.Center,
                        )
                    }
                    // Keyed by offset, not ID: offset paging can repeat a row when the list changes under it.
                    else -> items(items.count, key = items.key, contentType = { "card" }) { index ->
                        val content = items.get(index) ?: return@items
                        val next = content.continueInfo
                        // Offline, a volume that isn't downloaded can't be opened, and says so.
                        val unavailable = if (content.id in vm.unreadable) notDownloaded else null
                        val explain = { _: String -> snackbars.show(notDownloaded) }
                        ContentCard(
                            content,
                            if (unavailable != null) explain else openContent,
                            card,
                            series = next?.series,
                            isNew = next?.isNew == true,
                            onRead = (if (unavailable != null) explain else openReader).takeIf { next != null || parent != null },
                            parent = parent,
                            options = options,
                            highlightReading = true,
                            download = badges[content.id],
                            selecting = selection.active,
                            selected = content.id in selection.items,
                            onToggle = { toggle(content, next?.series?.title ?: parent?.title) },
                            unavailable = unavailable,
                            // The row's own key: the same item may sit in two rows, and it holds when earlier pages drop.
                            origin = items.key?.invoke(index)?.toString() ?: "i$index",
                        )
                    }
                }
                if (items.appendError != null) {
                    item(span = fullSpan, contentType = "error") { QueryError(items.appendError, retry = items.retry) }
                } else if (items.appending) {
                    item(span = fullSpan, contentType = "appending") {
                        Box(Modifier.fillMaxWidth().padding(8.dp), Alignment.Center) { VSpinner(Modifier.size(28.dp)) }
                    }
                }
            }
        }
        if (selection.active) {
            val busy = running != null
            val none = selection.items.isEmpty()
            val reason = stringResource(if (busy) R.string.bulk_busy else R.string.select_none)
            // The server lists a series' volumes and fetches its row, so Download needs it; comics only.
            val offline = needsConnection()
            val comics = selection.items.values.any { it.content.type == ContentType.COMIC || it.content.type == ContentType.COMIC_SERIES }
            val downloadReason = if (none) stringResource(R.string.select_none) else offline ?: stringResource(R.string.bulk_download_comics_only)
            val listable = selection.items.values.any { it.content.entryItem() != null }
            val listsReason = if (none) stringResource(R.string.select_none) else offline ?: stringResource(R.string.lists_none_addable)
            SelectionBar(
                listOf(
                    BarAction(VIcons.BookOpen, stringResource(R.string.bulk_status), { vm.openDialog(BulkDialog.Status) }, !none && !busy, reason),
                    BarAction(VIcons.Restart, stringResource(R.string.bulk_clear), { vm.openDialog(BulkDialog.Clear) }, !none && !busy, reason),
                    BarAction(VIcons.Download, stringResource(R.string.downloads_download), { vm.openDialog(BulkDialog.Download) }, comics && offline == null, downloadReason),
                    BarAction(VIcons.ListAlt, stringResource(R.string.lists_add_to_list), vm::openLists, listable && offline == null, listsReason),
                ),
                Modifier.align(Alignment.BottomCenter).onSizeChanged { barHeight = with(density) { it.height.toDp() } },
            )
        }
        // The panels open from the toolbar: one whose toolbar is scrolled away (after a rotation) is closed.
        LaunchedEffect(toolbarShown) { if (!toolbarShown && state.layoutInfo.totalItemsCount > 0) sheet = null }
    }
}

/**
 * Back leaves select mode, and the bulk dialogs. At screen level, outside any online/offline switch
 * around [ContentGrid]: going offline must not drop them while the selection or a batch lives on.
 */
@Composable
fun BulkSelectionHost(vm: GridViewModel, hostSheetEffects: Boolean = true) {
    val selection = vm.selection
    BackHandler(selection.active) { vm.selecting(false) }
    val mine by vm.ownBatch.collectAsStateWithLifecycle()
    val selected = selection.items.values
    // Outside the dialog's branch: its composition ends as Download is pressed, and the permission request and the snackbar come after.
    val snackbars = LocalSnackbars.current
    val openDownloads = LocalOffline.current.openDownloads
    val askNotifications = rememberAskNotifications()
    val context = LocalContext.current
    val view = stringResource(R.string.notices_view)
    val opening = vm.listsOpening
    if (opening != null) {
        val sheet = hiltViewModel<ListsSheetViewModel>(key = LISTS_SHEET)
        val activity = LocalActivity.current
        DisposableEffect(opening) {
            sheet.open(opening)
            onDispose { if (activity?.isChangingConfigurations != true) sheet.close(opening) }
        }
        ListsSheet(selected.mapNotNull { it.content.entryItem() }, selection = true, sheet, onDismiss = vm::closeLists)
    }
    if (hostSheetEffects) {
        // The sheet's writes outlive it: a selection's add ends select mode, and every message is shown here.
        val sheet = hiltViewModel<ListsSheetViewModel>(key = LISTS_SHEET)
        EffectHost(sheet.effects) { effect ->
            when (effect) {
                is SheetEffect.Message -> snackbars.show(effect.text.string(context))
                is SheetEffect.Added -> {
                    snackbars.show(effect.text.string(context))
                    vm.listsAdded(effect.opening)
                }
            }
        }
    }
    when (vm.dialog) {
        BulkDialog.Status -> BulkStatusDialog(selected.size, selection.mayHaveSeries, mine, vm::setStatus, vm::hide) { vm.openDialog(null) }
        BulkDialog.Clear -> BulkClearDialog(selected.map { it.content.title }, mine, vm::clear, vm::hide) { vm.openDialog(null) }
        BulkDialog.Download -> {
            val download = {
                askNotifications()
                vm.download { count, error ->
                    if (error != null) {
                        snackbars.show(error.string(context))
                    } else {
                        snackbars.show(context.resources.getQuantityString(R.plurals.bulk_download_queued, count, count), view, onAction = openDownloads)
                    }
                }
            }
            BulkDownloadDialog(vm.downloadCount, vm::countDownload, download) { vm.openDialog(null) }
        }
        null -> {}
    }
}

/**
 * [panel] is the open sheet's content, placed with its button so that a popover anchors to it and focus returns to it.
 * [stored]: a list from what is stored, which the server's filters can't apply to: only the display options.
 */
@Composable
private fun Toolbar(
    filters: GridFilters,
    total: Int?,
    open: GridSheet?,
    onOpen: (GridSheet) -> Unit,
    stored: Boolean,
    onStar: (Boolean) -> Unit,
    selecting: Boolean,
    onSelecting: (Boolean) -> Unit,
    panel: @Composable (GridSheet, PopoverAnchor) -> Unit,
) {
    // The first icon lines up with the cards' edge, as the web's toolbar does.
    Row(Modifier.offset(x = (-12).dp).padding(bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Box {
            val anchor = remember { PopoverAnchor() }
            VIconButton(VIcons.Tune, stringResource(R.string.grid_display), { onOpen(GridSheet.Display) }, Modifier.popoverAnchor(anchor))
            if (open == GridSheet.Display) panel(GridSheet.Display, anchor)
        }
        if (!stored) {
            Box {
                val anchor = remember { PopoverAnchor() }
                val active = stringResource(R.string.grid_filters_active)
                VIconButton(
                    VIcons.Filter,
                    stringResource(R.string.grid_filters),
                    { onOpen(GridSheet.Filters) },
                    Modifier.popoverAnchor(anchor).semantics { if (filters.hasFilters) stateDescription = active },
                )
                if (filters.hasFilters) {
                    Box(Modifier.align(Alignment.TopEnd).padding(10.dp).size(8.dp).background(MaterialTheme.colorScheme.primary, CircleShape))
                }
                if (open == GridSheet.Filters) panel(GridSheet.Filters, anchor)
            }
            VStarToggle(stringResource(R.string.grid_starred), filters.starred, onStar)
        }
        VIconToggle(VIcons.SelectMode, VIcons.SelectModeFilled, stringResource(R.string.select_items), selecting, onSelecting)
        Text(
            total?.let { pluralStringResource(R.plurals.grid_items, it, it) }.orEmpty(),
            Modifier.padding(start = 10.dp).semantics { liveRegion = LiveRegionMode.Polite },
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodyMedium,
        )
    }
}
