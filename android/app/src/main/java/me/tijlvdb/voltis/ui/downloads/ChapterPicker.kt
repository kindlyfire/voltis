package me.tijlvdb.voltis.ui.downloads

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.input.rememberTextFieldState
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Column
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.ui.kit.VChoiceRow
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VSearchField
import me.tijlvdb.voltis.ui.kit.VSheet
import me.tijlvdb.voltis.ui.theme.VoltisShapes
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/** A field that opens a picker: [label] over the chosen [value], or the label alone without one. It reads as "[label], [value]". */
@Composable
fun RangeField(label: String, value: String?, onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true) {
    val colors = MaterialTheme.colorScheme
    val none = stringResource(R.string.downloads_range_choose)
    Row(
        modifier
            .fillMaxWidth()
            .clip(VoltisShapes.field)
            .background(VoltisTheme.colors.field)
            .clickable(enabled, role = Role.DropdownList, onClick = onClick)
            .semantics {
                contentDescription = label
                stateDescription = value ?: none
            }
            .heightIn(min = 56.dp)
            .padding(start = 16.dp, end = 12.dp),
        Arrangement.spacedBy(8.dp),
        Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f).padding(vertical = 6.dp).clearAndSetSemantics {}) {
            Text(label, color = colors.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
            Text(
                value ?: none,
                color = if (value == null) colors.onSurfaceVariant else colors.onSurface,
                style = MaterialTheme.typography.bodyLarge,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Icon(VIcons.ChevronDown, contentDescription = null, tint = colors.onSurfaceVariant)
    }
}

/**
 * A sheet of [options] (a position and its name) under a search field that narrows them by name; it
 * opens at the [selected] one. Picking closes it.
 */
@Composable
fun ChapterPicker(title: String, options: List<Pair<Int, String>>, selected: Int?, onPick: (Int) -> Unit, onDismiss: () -> Unit) {
    val query = rememberTextFieldState()
    val shown = options.filter { it.second.contains(query.text.trim(), ignoreCase = true) }
    val list = rememberLazyListState(options.indexOfFirst { it.first == selected }.coerceAtLeast(0))
    VSheet(title, onDismiss, lazy = true) {
        VSearchField(query, stringResource(R.string.downloads_range_search), stringResource(R.string.downloads_range_search), Modifier.padding(horizontal = 24.dp, vertical = 4.dp))
        if (shown.isEmpty()) {
            Text(
                stringResource(R.string.downloads_range_no_match),
                Modifier.padding(24.dp),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodyMedium,
            )
        }
        LazyColumn(state = list, contentPadding = PaddingValues(bottom = 16.dp)) {
            items(shown, key = { it.first }) { (index, name) ->
                VChoiceRow(name, index == selected, { onPick(index); onDismiss() }, PaddingValues(horizontal = 24.dp), highlight = true)
            }
        }
    }
}
