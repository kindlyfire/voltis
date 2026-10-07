package me.tijlvdb.voltis.ui.grid

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.domain.catalog.GridOptions
import me.tijlvdb.voltis.domain.catalog.ItemCountMode
import me.tijlvdb.voltis.domain.catalog.columnRange
import me.tijlvdb.voltis.domain.catalog.gridColumns
import me.tijlvdb.voltis.domain.catalog.itemSizeFor
import me.tijlvdb.voltis.ui.kit.PopoverAnchor
import me.tijlvdb.voltis.ui.kit.VCheckboxRow
import me.tijlvdb.voltis.ui.kit.VIconButton
import me.tijlvdb.voltis.ui.kit.VIconButtonStyle
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VPopover
import me.tijlvdb.voltis.ui.kit.VSegmented
import me.tijlvdb.voltis.ui.theme.VoltisShapes
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/**
 * The web grid's display popover (`ContentGrid/Settings.vue`), as a popover on a tablet and a
 * sheet on a phone. The stepper shows the columns [gridWidth] (in dp) gives, and stores the item
 * size that gives the chosen count there.
 */
@Composable
fun DisplayOptionsSheet(
    options: GridOptions,
    gridWidth: Float,
    onChange: ((GridOptions) -> GridOptions) -> Unit,
    onReset: () -> Unit,
    onDismiss: () -> Unit,
    anchor: PopoverAnchor? = null,
) {
    VPopover(stringResource(R.string.grid_display), onDismiss, anchor = anchor) {
        // Resets every option, not only the columns.
        SheetSection(stringResource(R.string.display_columns), stringResource(R.string.reset), onAction = onReset)
        val columns = gridColumns(gridWidth, options.itemSize)
        val range = columnRange(gridWidth)
        val set = { n: Int -> onChange { it.copy(itemSize = itemSizeFor(gridWidth, n)) } }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            VIconButton(
                VIcons.Minus,
                stringResource(R.string.display_fewer_columns),
                { set(columns - 1) },
                style = VIconButtonStyle.Tonal,
                enabled = columns > range.first,
            )
            val count = pluralStringResource(R.plurals.display_column_count, columns, columns)
            Box(
                Modifier.weight(1f).height(48.dp).background(VoltisTheme.colors.field, VoltisShapes.field).clearAndSetSemantics {
                    contentDescription = count
                    liveRegion = LiveRegionMode.Polite
                },
                Alignment.Center,
            ) {
                Text(columns.toString(), style = MaterialTheme.typography.bodyLarge)
            }
            VIconButton(
                VIcons.Plus,
                stringResource(R.string.display_more_columns),
                { set(columns + 1) },
                style = VIconButtonStyle.Tonal,
                enabled = columns < range.last,
            )
        }

        HorizontalDivider(Modifier.padding(top = 16.dp, bottom = 4.dp))
        SheetSection(stringResource(R.string.display_visibility), stringResource(R.string.display_show_all)) {
            onChange { it.showAll() }
        }
        VCheckboxRow(stringResource(R.string.display_hide_count), options.hideItemCount, { on -> onChange { it.copy(hideItemCount = on) } })
        VCheckboxRow(stringResource(R.string.display_hide_status), options.hideStatus, { on -> onChange { it.copy(hideStatus = on) } })
        VCheckboxRow(
            stringResource(R.string.display_hide_highlight),
            options.hideReadingHighlight,
            { on -> onChange { it.copy(hideReadingHighlight = on) } },
        )
        VCheckboxRow(stringResource(R.string.display_hide_title), options.hideTitle, { on -> onChange { it.copy(hideTitle = on) } })
        VCheckboxRow(stringResource(R.string.display_hide_progress), options.hideProgress, { on -> onChange { it.copy(hideProgress = on) } })

        HorizontalDivider(Modifier.padding(top = 12.dp, bottom = 4.dp))
        SheetSection(stringResource(R.string.display_item_count))
        VSegmented(
            stringResource(R.string.display_item_count),
            listOf(
                ItemCountMode.Unread to stringResource(R.string.display_count_unread),
                ItemCountMode.Total to stringResource(R.string.display_count_total),
            ),
            options.itemCountMode,
            { mode -> onChange { it.copy(itemCountMode = mode) } },
        )
    }
}
