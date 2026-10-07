package me.tijlvdb.voltis.ui.discover

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.isImeVisible
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.input.rememberTextFieldState
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Facet
import me.tijlvdb.voltis.domain.catalog.FacetKind
import me.tijlvdb.voltis.domain.catalog.FacetSort
import me.tijlvdb.voltis.domain.catalog.facetLabel
import me.tijlvdb.voltis.ui.RefreshOnResume
import me.tijlvdb.voltis.ui.ServerOffline
import me.tijlvdb.voltis.ui.isOnline
import me.tijlvdb.voltis.ui.bottomSpace
import me.tijlvdb.voltis.ui.controlWidth
import me.tijlvdb.voltis.ui.isWide
import me.tijlvdb.voltis.ui.kit.PageHeader
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.SortOrder
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VSearchField
import me.tijlvdb.voltis.ui.kit.VSegmented
import me.tijlvdb.voltis.ui.kit.VSelect
import me.tijlvdb.voltis.ui.kit.VSortHeader
import me.tijlvdb.voltis.ui.kit.VSpinner
import me.tijlvdb.voltis.ui.kit.VTopBar
import me.tijlvdb.voltis.ui.pageGutter
import me.tijlvdb.voltis.ui.pageGutterBeforeIcon
import me.tijlvdb.voltis.ui.theme.VoltisShapes
import me.tijlvdb.voltis.ui.wideReadableWidth

/** Genres, tags, people and publishers with their counts: the port of `DiscoverPage.vue`. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun DiscoverScreen(
    onBack: () -> Unit,
    openFacet: (kind: String, key: String, libraryId: String?) -> Unit,
    vm: DiscoverViewModel = hiltViewModel(),
) {
    RefreshOnResume(vm.refresher)
    val query = vm.query
    val field = rememberTextFieldState(query.q)
    LaunchedEffect(field) { snapshotFlow { field.text.toString() }.collect(vm::setFilter) }
    val list = rememberLazyListState()
    val scrolled by remember { derivedStateOf { list.firstVisibleItemIndex > 0 } }
    // The next page once the last rows come into view, and again after each page while they still are.
    LaunchedEffect(list) {
        snapshotFlow {
            val info = list.layoutInfo
            val last = info.visibleItemsInfo.lastOrNull()?.index ?: -1
            (last >= info.totalItemsCount - 4) to vm.rows?.size
        }.collect { (end) -> if (end) vm.loadMore() }
    }
    val keyboard = LocalSoftwareKeyboardController.current
    LaunchedEffect(list.isScrollInProgress) { if (list.isScrollInProgress) keyboard?.hide() }
    val title = stringResource(R.string.discover)
    Surface(Modifier.fillMaxSize()) {
        Column(Modifier.imePadding()) {
            val online = isOnline()
            VTopBar(if (scrolled || !online) title else "", onBack)
            if (!online) return@Column ServerOffline()
            // Above the keyboard the navigation bar's share of bottomSpace() would be a gap.
            val bottom = if (WindowInsets.isImeVisible) 24.dp else bottomSpace()
            LazyColumn(Modifier.fillMaxSize(), list, PaddingValues(bottom = bottom)) {
                item(key = "controls", contentType = "controls") {
                    Column(Modifier.wideReadableWidth().padding(horizontal = pageGutter()), Arrangement.spacedBy(12.dp)) {
                        PageHeader(title, Modifier.padding(bottom = 4.dp))
                        val wide = isWide()
                        val kind = @Composable { VSegmented(stringResource(R.string.discover_kind), kindOptions(), query.kind, vm::setKind, block = !wide) }
                        val filter = @Composable { modifier: Modifier ->
                            VSearchField(
                                field,
                                stringResource(R.string.discover_filter),
                                stringResource(R.string.discover_filter_placeholder),
                                modifier,
                                busy = vm.rows == null && vm.error == null,
                            )
                        }
                        val library = @Composable { modifier: Modifier ->
                            VSelect(
                                stringResource(R.string.library),
                                vm.libraries.orEmpty().map { it.id to it.name },
                                query.library,
                                vm::setLibrary,
                                modifier,
                                clearLabel = stringResource(R.string.library_clear),
                                onClear = { vm.setLibrary(null) },
                                placeholder = stringResource(R.string.libraries_all),
                                compact = true,
                            )
                        }
                        if (wide) {
                            // One row as the web's from `sm`, wrapping where it doesn't fit; the library select keeps to the end.
                            FlowRow(
                                horizontalArrangement = Arrangement.spacedBy(12.dp),
                                verticalArrangement = Arrangement.spacedBy(12.dp),
                                itemVerticalAlignment = Alignment.CenterVertically,
                            ) {
                                kind()
                                filter(Modifier.width(controlWidth()))
                                Box(Modifier.weight(1f), Alignment.CenterEnd) { library(Modifier.width(controlWidth())) }
                            }
                        } else {
                            kind()
                            filter(Modifier)
                            library(Modifier)
                        }
                        QueryError(vm.librariesError, retry = vm::loadLibraries)
                        // Always there, so a new total is announced.
                        Text(
                            vm.total?.let { pluralStringResource(totalPlural(query.kind), it, it) }.orEmpty(),
                            Modifier.semantics { liveRegion = LiveRegionMode.Polite },
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            style = MaterialTheme.typography.bodyMedium,
                        )
                    }
                }
                vm.error?.let {
                    item(key = "error", contentType = "error") {
                        QueryError(it, Modifier.wideReadableWidth().padding(horizontal = pageGutter()).padding(bottom = 8.dp), vm::reload)
                    }
                }
                item(key = "sort", contentType = "sort") { SortHeader(query.sort, query.order, vm::toggleSort) }
                val rows = vm.rows
                when {
                    rows == null -> if (vm.error == null) items(8, contentType = { "skeleton" }) { SkeletonRow(it == 0) }
                    rows.isEmpty() -> item(key = "empty", contentType = "empty") {
                        Text(
                            stringResource(if (query.q.isEmpty()) R.string.discover_none else R.string.discover_no_matches),
                            Modifier.wideReadableWidth().padding(vertical = 48.dp),
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            textAlign = TextAlign.Center,
                        )
                    }
                    else -> items(rows, key = { "facet:${query.kind}:${it.key}" }, contentType = { "row" }) { row ->
                        FacetRow(query.kind, row) { openFacet(query.kind, row.key, query.library) }
                    }
                }
                vm.appendError?.let {
                    item(key = "append-error", contentType = "error") {
                        QueryError(it, Modifier.wideReadableWidth().padding(horizontal = pageGutter(), vertical = 16.dp), vm::retryMore)
                    }
                }
                if (vm.stalled) {
                    item(key = "load-more", contentType = "load-more") {
                        Box(Modifier.wideReadableWidth().padding(8.dp), Alignment.Center) {
                            VButton(stringResource(R.string.discover_load_more), vm::loadMore, style = VButtonStyle.Text)
                        }
                    }
                }
                if (vm.appending) {
                    item(key = "appending", contentType = "appending") {
                        Box(Modifier.wideReadableWidth().padding(8.dp), Alignment.Center) { VSpinner(Modifier.size(28.dp)) }
                    }
                }
            }
        }
    }
}

@Composable
private fun kindOptions() = listOf(
    FacetKind.GENRES to stringResource(R.string.facet_genres),
    FacetKind.TAGS to stringResource(R.string.facet_tags),
    FacetKind.PEOPLE to stringResource(R.string.facet_people),
    FacetKind.PUBLISHERS to stringResource(R.string.facet_publishers),
)

private fun totalPlural(kind: String) = when (kind) {
    FacetKind.TAGS -> R.plurals.discover_total_tags
    FacetKind.PEOPLE -> R.plurals.discover_total_people
    FacetKind.PUBLISHERS -> R.plurals.discover_total_publishers
    else -> R.plurals.discover_total_genres
}

/** Name at the start, Count at the end, over the rows' columns. */
@Composable
private fun SortHeader(sort: String, order: String, onToggle: (String) -> Unit) {
    Column(Modifier.wideReadableWidth()) {
        Row(Modifier.fillMaxWidth().padding(horizontal = pageGutterBeforeIcon()), verticalAlignment = Alignment.CenterVertically) {
            val sorted = if (order == FacetSort.ASC) SortOrder.Ascending else SortOrder.Descending
            VSortHeader(stringResource(R.string.discover_name), sorted.takeIf { sort == FacetSort.NAME }, { onToggle(FacetSort.NAME) })
            Spacer(Modifier.weight(1f))
            VSortHeader(stringResource(R.string.discover_count), sorted.takeIf { sort == FacetSort.COUNT }, { onToggle(FacetSort.COUNT) })
        }
        HorizontalDivider(Modifier.padding(horizontal = pageGutter()), color = MaterialTheme.colorScheme.outlineVariant)
    }
}

@Composable
private fun FacetRow(kind: String, row: Facet, onClick: () -> Unit) {
    val name = facetLabel(kind, row.name)
    val description = stringResource(R.string.discover_row, name, pluralStringResource(R.plurals.grid_items, row.count, row.count))
    Row(
        Modifier
            .wideReadableWidth()
            .clickable(role = Role.Button, onClick = onClick)
            .semantics { contentDescription = description }
            .heightIn(min = 48.dp)
            .padding(horizontal = pageGutter(), vertical = 10.dp),
        Arrangement.spacedBy(16.dp),
        Alignment.CenterVertically,
    ) {
        Text(name, Modifier.weight(1f).clearAndSetSemantics {}, style = MaterialTheme.typography.bodyLarge)
        Text(
            row.count.toString(),
            Modifier.clearAndSetSemantics {},
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodyMedium,
        )
    }
}

/** A row's shape while the first page loads; the first one says so. */
@Composable
private fun SkeletonRow(first: Boolean) {
    val label = stringResource(R.string.loading)
    val color = MaterialTheme.colorScheme.surfaceContainerHigh
    Row(
        Modifier
            .wideReadableWidth()
            .then(if (first) Modifier.semantics { contentDescription = label } else Modifier)
            .heightIn(min = 48.dp)
            .padding(horizontal = pageGutter(), vertical = 15.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.fillMaxWidth(0.5f).height(16.dp).background(color, VoltisShapes.menuItem))
        Spacer(Modifier.weight(1f))
        Box(Modifier.size(24.dp, 16.dp).background(color, VoltisShapes.menuItem))
    }
}
