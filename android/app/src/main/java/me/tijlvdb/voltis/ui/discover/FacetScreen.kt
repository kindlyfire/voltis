package me.tijlvdb.voltis.ui.discover

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.paging.compose.collectAsLazyPagingItems
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.FacetEntry
import me.tijlvdb.voltis.domain.catalog.FacetKind
import me.tijlvdb.voltis.domain.catalog.facetLabel
import me.tijlvdb.voltis.domain.catalog.slugLabel
import me.tijlvdb.voltis.ui.LoadStatus
import me.tijlvdb.voltis.ui.ServerOffline
import me.tijlvdb.voltis.ui.isOnline
import me.tijlvdb.voltis.ui.RefreshOnResume
import me.tijlvdb.voltis.ui.controlWidth
import me.tijlvdb.voltis.ui.grid.BulkSelectionHost
import me.tijlvdb.voltis.ui.grid.ContentGrid
import me.tijlvdb.voltis.ui.grid.GridSource
import me.tijlvdb.voltis.ui.grid.GridViewModel
import me.tijlvdb.voltis.ui.grid.SelectionTopBar
import me.tijlvdb.voltis.ui.grid.asGridItems
import me.tijlvdb.voltis.ui.isShort
import me.tijlvdb.voltis.ui.isWide
import me.tijlvdb.voltis.ui.kit.PageHeader
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VChip
import me.tijlvdb.voltis.ui.kit.VSelect
import me.tijlvdb.voltis.ui.kit.VTopBar
import me.tijlvdb.voltis.ui.pageGutter

/** A Discover value's grid: the port of `FacetPage.vue`. [libraryId] is the library it opens in. */
@Composable
fun FacetScreen(
    kind: String,
    key: String,
    libraryId: String?,
    onBack: () -> Unit,
    openContent: (contentId: String) -> Unit,
    openReader: (contentId: String) -> Unit,
    vm: FacetViewModel = hiltViewModel<FacetViewModel, FacetViewModel.Factory> { it.create(kind, key) },
    grid: GridViewModel = hiltViewModel<GridViewModel, GridViewModel.Factory> { it.create(GridSource.Facet(kind, key, libraryId)) },
) {
    RefreshOnResume(vm.refresher)
    RefreshOnResume(grid.refresher)
    val scope = grid.facetScope
    // The count and the roles are the library's.
    LaunchedEffect(scope.libraryId) { vm.show(scope.libraryId) }
    val entry = vm.entry
    val title = entry?.let { facetLabel(kind, it.name) }.orEmpty()
    val state = rememberLazyGridState()
    val scrolled by remember { derivedStateOf { state.firstVisibleItemIndex > 0 } }
    // A phone on its side has no height for the page's title: the bar has it from the start.
    val short = isShort()
    BulkSelectionHost(grid)
    Surface(Modifier.fillMaxSize()) {
        Column {
            val online = isOnline()
            if (grid.selection.active) SelectionTopBar(grid.selection.items.size) { grid.selecting(false) } else VTopBar(if (scrolled || short || !online) title else "", onBack)
            when {
                !online -> ServerOffline()
                vm.notFound -> NotFound(onBack)
                entry == null -> Box(Modifier.fillMaxSize().padding(horizontal = pageGutter(), vertical = 16.dp), Alignment.TopCenter) { LoadStatus(true, vm.error, vm::reload) }
                else -> ContentGrid(
                    grid,
                    grid.pages.collectAsLazyPagingItems().asGridItems(),
                    openContent,
                    openReader,
                    state = state,
                    onRefresh = {
                        vm.reload()
                        grid.refresh()
                    },
                ) {
                    item(span = { GridItemSpan(maxLineSpan) }, contentType = "header") {
                        Column(Modifier.padding(bottom = 8.dp), Arrangement.spacedBy(12.dp)) {
                            QueryError(vm.error, retry = vm::reload)
                            if (!short) PageHeader(title)
                            // Under the title, as the web's header toolbar: full width on a phone, `sm:w-56` wider.
                            VSelect(
                                stringResource(R.string.library),
                                vm.libraries.orEmpty().map { it.id to it.name },
                                scope.libraryId,
                                { grid.applyFacetScope(scope.copy(libraryId = it)) },
                                if (isWide()) Modifier.width(controlWidth()) else Modifier.fillMaxWidth(),
                                clearLabel = stringResource(R.string.library_clear),
                                onClear = { grid.applyFacetScope(scope.copy(libraryId = null)) },
                                placeholder = stringResource(R.string.libraries_all),
                                compact = true,
                            )
                            QueryError(vm.librariesError, retry = vm::loadLibraries)
                        }
                    }
                    if (kind == FacetKind.PEOPLE && (entry.roles.isNotEmpty() || scope.role != null)) {
                        item(span = { GridItemSpan(maxLineSpan) }, contentType = "roles") {
                            RoleChips(entry, scope.role) { grid.applyFacetScope(scope.copy(role = it)) }
                        }
                    }
                }
            }
        }
    }
}

/** "All (N)", then each role with its count; one is chosen. */
@Composable
private fun RoleChips(entry: FacetEntry, role: String?, onSelect: (String?) -> Unit) {
    FlowRow(Modifier.fillMaxWidth().padding(bottom = 4.dp), Arrangement.spacedBy(8.dp)) {
        VChip(stringResource(R.string.facet_role_all, entry.count), role == null, { onSelect(null) })
        for (count in entry.roles) {
            VChip(stringResource(R.string.facet_role, slugLabel(count.role), count.count), role == count.role, { onSelect(count.role) })
        }
    }
}

@Composable
private fun NotFound(onBack: () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(horizontal = pageGutter(), vertical = 16.dp).padding(top = 48.dp), Arrangement.spacedBy(16.dp), Alignment.CenterHorizontally) {
        Text(stringResource(R.string.facet_not_found), Modifier.semantics { heading() }, style = MaterialTheme.typography.headlineMedium)
        VButton(stringResource(R.string.back), onBack)
    }
}
