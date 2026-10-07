package me.tijlvdb.voltis.ui.lists

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.content.coverUrl
import me.tijlvdb.voltis.data.lists.ListView
import me.tijlvdb.voltis.ui.EffectHost
import me.tijlvdb.voltis.ui.OfflineBar
import me.tijlvdb.voltis.ui.RefreshOnResume
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.needsConnection
import me.tijlvdb.voltis.ui.bottomSpace
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.PageHeader
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VDisabled
import me.tijlvdb.voltis.ui.kit.VIconButton
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VCard
import me.tijlvdb.voltis.ui.kit.VCover
import me.tijlvdb.voltis.ui.kit.VTopBar
import me.tijlvdb.voltis.ui.isWide
import me.tijlvdb.voltis.ui.pageGutter
import me.tijlvdb.voltis.ui.theme.VoltisShapes

/** The user's lists (the web's `ListsPage.vue`), from the cache. Create and the edit buttons need the server. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ListsScreen(onBack: () -> Unit, openList: (String) -> Unit, vm: ListsViewModel = hiltViewModel()) {
    RefreshOnResume(vm.refresher)
    val lists by vm.lists.collectAsStateWithLifecycle()
    val storeFailed by vm.storeFailed.collectAsStateWithLifecycle()
    val online by vm.online.collectAsStateWithLifecycle()
    val snackbars = LocalSnackbars.current
    val context = LocalContext.current
    EffectHost(vm.effects) { if (it is ListEffect.Message) snackbars.show(it.text.string(context)) }
    val state = rememberLazyGridState()
    val scrolled by remember { derivedStateOf { state.firstVisibleItemIndex > 0 } }
    val title = stringResource(R.string.lists_title)
    Surface(Modifier.fillMaxSize()) {
        Column {
            VTopBar(if (scrolled) title else "", onBack)
            PullToRefreshBox(vm.refreshing, { vm.refresh() }) {
                // One column on a phone; on a wide window as many 320 dp ones as fit, as the web's 2 or 3.
                val gutter = pageGutter()
                LazyVerticalGrid(
                    if (isWide()) GridCells.Adaptive(320.dp) else GridCells.Fixed(1),
                    Modifier.fillMaxSize(),
                    state,
                    PaddingValues(start = gutter, end = gutter, bottom = bottomSpace()),
                    verticalArrangement = Arrangement.spacedBy(12.dp),
                    horizontalArrangement = Arrangement.spacedBy(16.dp),
                ) {
                    item(key = "header", span = { GridItemSpan(maxLineSpan) }, contentType = "header") {
                        Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                            FlowRow(
                                Modifier.fillMaxWidth(),
                                Arrangement.spacedBy(12.dp, Alignment.End),
                                Arrangement.spacedBy(8.dp),
                                itemVerticalAlignment = Alignment.CenterVertically,
                            ) {
                                PageHeader(title, Modifier.weight(1f))
                                VDisabled(needsConnection(), stringResource(R.string.lists_create)) { enabled ->
                                    VButton(stringResource(R.string.lists_create), { vm.editor.show(ListDialog.Create) }, enabled = enabled, icon = VIcons.Plus)
                                }
                            }
                            OfflineBar()
                            QueryError(vm.error, retry = { vm.refresh(pulled = false) })
                        }
                    }
                    val current = lists
                    when {
                        storeFailed != null -> item(key = "store", span = { GridItemSpan(maxLineSpan) }, contentType = "error") {
                            QueryError(UiText.Res(R.string.error_offline_data))
                        }
                        // The cache isn't read yet, or the account's store is still opening.
                        current == null -> items(3, contentType = { "skeleton" }) { SkeletonCard(it == 0) }
                        current.isEmpty() && vm.busy && vm.error == null -> items(3, contentType = { "skeleton" }) { SkeletonCard(it == 0) }
                        current.isEmpty() && vm.error == null -> item(key = "empty", span = { GridItemSpan(maxLineSpan) }, contentType = "empty") {
                            Column(Modifier.padding(vertical = 24.dp), Arrangement.spacedBy(4.dp)) {
                                Text(stringResource(R.string.lists_empty), style = MaterialTheme.typography.titleMedium)
                                Text(stringResource(R.string.lists_empty_text), color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                        current.isEmpty() -> Unit
                        else -> items(current, key = { it.id }, contentType = { "list" }) { list ->
                            ListCard(list, Modifier, { vm.editor.show(ListDialog.Edit(list)) }) {
                                openList(list.id)
                            }
                        }
                    }
                }
            }
        }
    }
    ListDialogs(vm.editor, online)
}

@Composable
private fun ListCard(list: ListView, modifier: Modifier, onEdit: () -> Unit, onClick: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    // At least the cover's height (2:3) and the card's padding, so short cards in a grid row line up.
    VCard(onClick, modifier.heightIn(min = CoverWidth * 1.5f + 24.dp)) {
        VCover(list.cover?.let { coverUrl(it.id, it.coverVersion) }, Modifier.width(CoverWidth))
        // The card reads as one: the name, the count, the visibility and the date.
        Column(Modifier.weight(1f), Arrangement.spacedBy(4.dp)) {
            Text(list.name, style = MaterialTheme.typography.titleMedium, maxLines = 3, overflow = TextOverflow.Ellipsis)
            Text(entriesLabel(list), color = muted, style = MaterialTheme.typography.bodyMedium)
            Text(visibilityLabel(list.visibility), color = muted, style = MaterialTheme.typography.bodyMedium)
            Text(updatedLabel(list), color = muted, style = MaterialTheme.typography.bodyMedium)
        }
        val edit = stringResource(R.string.lists_edit_named, list.name)
        VDisabled(needsConnection(), edit, Modifier.offset(x = 4.dp, y = (-4).dp)) { enabled -> VIconButton(VIcons.Edit, edit, onEdit, enabled = enabled) }
    }
}

/** A card's shape while the first refresh runs with nothing cached; the first one says so. */
@Composable
private fun SkeletonCard(first: Boolean) {
    val label = stringResource(R.string.loading)
    val color = MaterialTheme.colorScheme.surfaceContainerHigh
    Row(
        Modifier
            .then(if (first) Modifier.semantics { contentDescription = label } else Modifier)
            .padding(12.dp),
        Arrangement.spacedBy(16.dp),
    ) {
        Box(Modifier.width(80.dp).height(120.dp).background(color, VoltisShapes.cover))
        Column(Modifier.weight(1f), Arrangement.spacedBy(8.dp)) {
            Box(Modifier.fillMaxWidth(0.6f).height(20.dp).background(color, VoltisShapes.menuItem))
            Box(Modifier.fillMaxWidth(0.3f).height(14.dp).background(color, VoltisShapes.menuItem))
        }
    }
}

private val CoverWidth = 80.dp
