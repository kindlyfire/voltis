package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.selection.selectable
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp

/** One choice of a single-choice list: [label], with a check while [selected]. [highlight] also colours the selected label. */
@Composable
fun VChoiceRow(
    label: String,
    selected: Boolean,
    onClick: () -> Unit,
    padding: PaddingValues = PaddingValues(),
    highlight: Boolean = false,
) {
    val primary = MaterialTheme.colorScheme.primary
    Row(
        Modifier.fillMaxWidth().selectable(selected, role = Role.RadioButton, onClick = onClick).heightIn(min = 48.dp).padding(padding),
        Arrangement.spacedBy(12.dp),
        Alignment.CenterVertically,
    ) {
        Text(label, Modifier.weight(1f), color = if (selected && highlight) primary else Color.Unspecified, style = MaterialTheme.typography.bodyLarge)
        if (selected) Icon(VIcons.Check, contentDescription = null, tint = primary)
    }
}
