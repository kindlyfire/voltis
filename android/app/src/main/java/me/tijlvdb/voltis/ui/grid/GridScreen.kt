package me.tijlvdb.voltis.ui.grid

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.layout.onPlaced
import androidx.compose.ui.layout.positionInParent
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.ui.OnlineOnly
import me.tijlvdb.voltis.ui.RefreshOnResume
import me.tijlvdb.voltis.ui.isOnline
import me.tijlvdb.voltis.ui.isShort
import me.tijlvdb.voltis.ui.kit.PageHeader
import me.tijlvdb.voltis.ui.kit.VTopBar

/** A library's grid, or All libraries'. */
@Composable
fun GridScreen(
    source: GridSource,
    onBack: () -> Unit,
    openContent: (contentId: String) -> Unit,
    openReader: (contentId: String) -> Unit,
    vm: GridViewModel = hiltViewModel<GridViewModel, GridViewModel.Factory> { it.create(source) },
) {
    RefreshOnResume(vm.refresher)
    val title = when (source) {
        is GridSource.Browse -> stringResource(R.string.libraries_all)
        // A series' contents are part of its page, not a screen of their own.
        else -> vm.libraryName ?: stringResource(R.string.library)
    }
    val state = rememberLazyGridState()
    // The bar takes the title once the page's own has scrolled away.
    var titleEnd by remember { mutableIntStateOf(0) }
    val scrolled by remember { derivedStateOf { val s = state.headerScroll(); s == null || titleEnd > 0 && s >= titleEnd } }
    // A phone on its side has no height for the page's title: the bar has it from the start.
    val short = isShort()
    BulkSelectionHost(vm)
    Surface(Modifier.fillMaxSize()) {
        Column {
            val online = isOnline()
            if (vm.selection.active) SelectionTopBar(vm.selection.items.size) { vm.selecting(false) } else VTopBar(if (scrolled || short || !online) title else "", onBack)
            OnlineOnly {
                ContentGrid(vm, vm.windowItems(), openContent, openReader, state = state, top = {
                    if (!short) PageHeader(title, Modifier.padding(bottom = 4.dp).onPlaced { titleEnd = it.positionInParent().y.toInt() + it.size.height })
                })
            }
        }
    }
}
