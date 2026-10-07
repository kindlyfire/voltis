package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material3.Checkbox
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp

/** The checkbox, then its label, and [meta] (a size, a state) at the end. The whole row toggles; disabled, all of it dims. */
@Composable
fun VCheckboxRow(label: String, checked: Boolean, onChange: (Boolean) -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true, meta: String? = null) {
    Row(
        modifier.fillMaxWidth().toggleable(checked, enabled, Role.Checkbox, onValueChange = onChange).heightIn(min = 48.dp),
        Arrangement.spacedBy(16.dp),
        Alignment.CenterVertically,
    ) {
        Checkbox(checked, onCheckedChange = null, enabled = enabled)
        // Disabled, it reads as disabled as the box does.
        val dim = if (enabled) 1f else 0.38f
        val colors = MaterialTheme.colorScheme
        Text(label, Modifier.weight(1f), color = colors.onSurface.copy(alpha = dim), style = MaterialTheme.typography.bodyLarge)
        if (meta != null) Text(meta, color = colors.onSurfaceVariant.copy(alpha = dim), style = MaterialTheme.typography.bodyMedium)
    }
}
