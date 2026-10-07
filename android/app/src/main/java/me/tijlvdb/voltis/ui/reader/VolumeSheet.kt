package me.tijlvdb.voltis.ui.reader

import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.domain.catalog.itemName
import me.tijlvdb.voltis.ui.itemLabels
import me.tijlvdb.voltis.ui.kit.VChoiceRow
import me.tijlvdb.voltis.ui.kit.VSheet

/**
 * A sheet listing [volumes] by their names within [series], the one at [selected] marked (-1 for
 * none). Lazy: a series can have hundreds. Choosing the marked one only closes the sheet.
 */
@Composable
fun VolumeSheet(title: String, volumes: List<Content>, series: Content?, selected: Int, onPick: (String) -> Unit, onDismiss: () -> Unit) {
    VSheet(title, onDismiss, meta = if (selected < 0) null else stringResource(R.string.reader_page_of, selected + 1, volumes.size), lazy = true) {
        val labels = itemLabels()
        LazyColumn(
            Modifier.selectableGroup(),
            rememberLazyListState((selected - 2).coerceAtLeast(0)),
        ) {
            itemsIndexed(volumes, key = { _, volume -> volume.id }) { index, volume ->
                val current = index == selected
                VChoiceRow(
                    itemName(volume, series, labels),
                    current,
                    { if (current) onDismiss() else onPick(volume.id) },
                    PaddingValues(horizontal = 24.dp, vertical = 12.dp),
                    highlight = true,
                )
            }
        }
    }
}
