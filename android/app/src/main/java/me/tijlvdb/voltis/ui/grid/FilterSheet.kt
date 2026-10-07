package me.tijlvdb.voltis.ui.grid

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.content.ContentSort
import me.tijlvdb.voltis.data.content.GridFilters
import me.tijlvdb.voltis.ui.kit.PopoverAnchor
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VChip
import me.tijlvdb.voltis.ui.kit.VChoiceRow
import me.tijlvdb.voltis.ui.kit.VPopover
import me.tijlvdb.voltis.ui.kit.VSegmented
import me.tijlvdb.voltis.ui.statusLabels

/**
 * The web grid's row of Status, Rating and Sort selects, as a sheet on a phone and a popover on
 * a tablet. Changes apply at once. [sorts] is empty for a grid whose sort is fixed, which then
 * offers only the order.
 */
@Composable
fun FilterSheet(
    filters: GridFilters,
    sorts: List<String>,
    onChange: (GridFilters) -> Unit,
    onDismiss: () -> Unit,
    anchor: PopoverAnchor? = null,
) {
    VPopover(
        stringResource(R.string.grid_filters),
        onDismiss,
        width = 360.dp,
        anchor = anchor,
        // A text button's label is 12 dp inside it: it lines up with the content's edge.
        action = {
            VButton(stringResource(R.string.reset), { onChange(filters.reset()) }, Modifier.offset(x = 12.dp), VButtonStyle.Text, enabled = filters.hasFilters)
        },
    ) {
        SheetSection(stringResource(R.string.filter_status))
        Chips(
            listOf(
                GridFilters.YES to stringResource(R.string.filter_has_status),
                GridFilters.NO to stringResource(R.string.filter_no_status),
            ) + statusLabels(),
            filters.status,
        ) { onChange(filters.copy(status = it)) }

        SheetSection(stringResource(R.string.filter_rating))
        Chips(
            listOf(
                GridFilters.YES to stringResource(R.string.filter_has_rating),
                GridFilters.NO to stringResource(R.string.filter_no_rating),
            ),
            filters.rating,
        ) { onChange(filters.copy(rating = it)) }

        SheetSection(stringResource(R.string.filter_sort))
        VSegmented(
            stringResource(R.string.filter_sort),
            listOf(
                GridFilters.ASC to stringResource(R.string.filter_ascending),
                GridFilters.DESC to stringResource(R.string.filter_descending),
            ),
            filters.sortOrder,
            { onChange(filters.copy(sortOrder = it)) },
            Modifier.padding(bottom = 8.dp),
        )
        Column(Modifier.selectableGroup()) {
            for (sort in sorts) {
                val selected = sort == filters.sort
                // Choosing the current sort again would only reset its order.
                VChoiceRow(stringResource(sortLabel(sort)), selected, { if (!selected) onChange(filters.withSort(sort)) })
            }
        }
    }
}

/** A section's label in a grid sheet, with an optional text button at its end. */
@Composable
internal fun SheetSection(title: String, action: String? = null, onAction: () -> Unit = {}) {
    Row(Modifier.fillMaxWidth().heightIn(min = 48.dp), Arrangement.SpaceBetween, Alignment.CenterVertically) {
        Text(
            title,
            Modifier.semantics { heading() },
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.titleSmall,
        )
        if (action != null) VButton(action, onAction, style = VButtonStyle.Text)
    }
}

/** One choice at most: a tap on the selected chip clears it. */
@Composable
private fun Chips(options: List<Pair<String, String>>, selected: String?, onSelect: (String?) -> Unit) {
    FlowRow(Modifier.selectableGroup(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        for ((value, label) in options) {
            VChip(label, value == selected, { onSelect(value.takeIf { it != selected }) })
        }
    }
}

private fun sortLabel(sort: String) = when (sort) {
    ContentSort.CONTINUE -> R.string.home_continue
    ContentSort.RECENTLY_UPDATED -> R.string.home_updated
    ContentSort.HISTORY -> R.string.sort_history
    ContentSort.CREATED_AT -> R.string.home_added
    ContentSort.LAST_READ_AT -> R.string.sort_last_read
    ContentSort.RATING -> R.string.filter_rating
    ContentSort.USER_RATING -> R.string.sort_user_rating
    ContentSort.RELEASE_DATE -> R.string.sort_release_date
    ContentSort.UNREAD_COUNT -> R.string.sort_unread_count
    else -> R.string.sort_title
}
