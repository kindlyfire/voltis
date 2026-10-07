package me.tijlvdb.voltis.ui.search

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.isImeVisible
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.input.rememberTextFieldState
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.content.coverUrl
import me.tijlvdb.voltis.domain.catalog.GridOptions
import me.tijlvdb.voltis.domain.catalog.gridColumns
import me.tijlvdb.voltis.ui.ServerOffline
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.isOnline
import me.tijlvdb.voltis.ui.bottomSpace
import me.tijlvdb.voltis.ui.grid.ColumnGap
import me.tijlvdb.voltis.ui.grid.ContentCard
import me.tijlvdb.voltis.ui.nav.HeroKey
import me.tijlvdb.voltis.ui.nav.LocalHeroOrigin
import me.tijlvdb.voltis.ui.nav.LocalHeroTaps
import me.tijlvdb.voltis.ui.grid.RowGap
import me.tijlvdb.voltis.ui.isLarge
import me.tijlvdb.voltis.ui.kit.PageHeader
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VCover
import me.tijlvdb.voltis.ui.kit.VSearchField
import me.tijlvdb.voltis.ui.pageGutter
import me.tijlvdb.voltis.ui.readableWidth
import me.tijlvdb.voltis.ui.topInset
import me.tijlvdb.voltis.ui.typeLabel

/** The web's search box as a tab: a result opens its page in this tab's stack. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun SearchScreen(openContent: (contentId: String) -> Unit, vm: SearchViewModel = hiltViewModel()) {
    val field = rememberTextFieldState()
    LaunchedEffect(field) { snapshotFlow { field.text.toString() }.collect(vm::setTerm) }
    val focus = remember { FocusRequester() }
    val online = isOnline()
    // Not on the way back from a result: the keyboard would cover the list again. Offline there is no field.
    LaunchedEffect(Unit) { if (online && field.text.isEmpty()) focus.requestFocus() }
    // A search that failed offline goes again once the server is back.
    LaunchedEffect(online) { if (online && vm.failed) vm.retry() }
    Surface(Modifier.fillMaxSize()) {
        Column(Modifier.topInset().imePadding()) {
            if (!online) {
                Column(Modifier.readableWidth(pageGutter()).padding(horizontal = pageGutter())) { PageHeader(stringResource(R.string.tab_search)) }
                return@Column ServerOffline()
            }
            Column(Modifier.readableWidth(pageGutter()).padding(horizontal = pageGutter()).padding(bottom = 8.dp), Arrangement.spacedBy(12.dp)) {
                PageHeader(stringResource(R.string.tab_search))
                VSearchField(
                    field,
                    stringResource(R.string.tab_search),
                    stringResource(R.string.search_placeholder),
                    Modifier.focusRequester(focus),
                    busy = vm.searching,
                )
                if (vm.failed) QueryError(UiText.Res(R.string.search_failed), retry = vm::retry)
                val results = vm.results
                val status = when {
                    vm.searching -> stringResource(R.string.search_searching)
                    results == null -> ""
                    results.isEmpty() -> stringResource(R.string.search_no_results)
                    else -> pluralStringResource(R.plurals.search_results, results.size, results.size)
                }
                // Always there, so a change of its text is announced: "Searching…", then the term's count.
                Text(
                    status,
                    Modifier.semantics { liveRegion = LiveRegionMode.Polite },
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
            val keyboard = LocalSoftwareKeyboardController.current
            // Above the keyboard the navigation bar's share of bottomSpace() would be a gap.
            val bottom = if (WindowInsets.isImeVisible) 24.dp else bottomSpace()
            // A tablet has room for covers: the results are cards, as a library's grid.
            if (isLarge()) {
                val grid = rememberLazyGridState()
                // The keyboard covers the lower results: scrolling them puts it away.
                LaunchedEffect(grid.isScrollInProgress) { if (grid.isScrollInProgress) keyboard?.hide() }
                val gutter = pageGutter()
                val badges by vm.badges.collectAsStateWithLifecycle()
                BoxWithConstraints {
                    LazyVerticalGrid(
                        GridCells.Fixed(gridColumns((maxWidth - gutter * 2).value, GridOptions().itemSize)),
                        state = grid,
                        contentPadding = PaddingValues(start = gutter, top = 4.dp, end = gutter, bottom = bottom),
                        horizontalArrangement = Arrangement.spacedBy(ColumnGap),
                    ) {
                        items(vm.results.orEmpty(), key = { it.id }) {
                            ContentCard(it, openContent, Modifier.padding(bottom = RowGap), download = badges[it.id], subtitle = typeLabel(it.type))
                        }
                    }
                }
            } else {
                val list = rememberLazyListState()
                LaunchedEffect(list.isScrollInProgress) { if (list.isScrollInProgress) keyboard?.hide() }
                LazyColumn(state = list, contentPadding = PaddingValues(top = 4.dp, bottom = bottom)) {
                    items(vm.results.orEmpty(), key = { it.id }) { ResultRow(it) { openContent(it.id) } }
                }
            }
        }
    }
}

@Composable
private fun ResultRow(item: Content, onClick: () -> Unit) {
    val hero = HeroKey(item.id, LocalHeroOrigin.current + "/row")
    val taps = LocalHeroTaps.current
    Row(
        Modifier.readableWidth(pageGutter()).clickable(role = Role.Button) {
            taps.tap(hero)
            onClick()
        }.padding(horizontal = pageGutter(), vertical = 6.dp),
        Arrangement.spacedBy(12.dp),
        Alignment.CenterVertically,
    ) {
        VCover(coverUrl(item), Modifier.width(40.dp), hero)
        Column {
            Text(
                item.title,
                style = MaterialTheme.typography.bodyMedium.copy(fontWeight = FontWeight.Medium),
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            Text(typeLabel(item.type), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
        }
    }
}
