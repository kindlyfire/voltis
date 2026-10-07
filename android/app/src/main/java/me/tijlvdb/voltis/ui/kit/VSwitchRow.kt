package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp

/** A settings row: the label, then the switch. The whole row toggles. */
@Composable
fun VSwitchRow(label: String, checked: Boolean, onChange: (Boolean) -> Unit, modifier: Modifier = Modifier) {
    val scheme = MaterialTheme.colorScheme
    Row(
        modifier.fillMaxWidth().toggleable(checked, role = Role.Switch, onValueChange = onChange).heightIn(min = 48.dp),
        Arrangement.spacedBy(16.dp),
        Alignment.CenterVertically,
    ) {
        Text(label, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
        Switch(
            checked,
            onCheckedChange = null,
            // The web's thumb is white with a shadow. Material's has none, so off it takes the outline color to stay visible.
            colors = SwitchDefaults.colors(
                checkedThumbColor = Color.White,
                checkedTrackColor = scheme.primary,
                uncheckedThumbColor = scheme.outline,
                uncheckedTrackColor = scheme.surfaceContainerHighest,
                uncheckedBorderColor = scheme.outline,
            ),
        )
    }
}
