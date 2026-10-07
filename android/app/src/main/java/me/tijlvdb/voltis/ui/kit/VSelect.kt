package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.ui.theme.VoltisShapes
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/**
 * A field that opens a menu of [options]. It is named [label] and says its value under it, or
 * [placeholder] without one. [compact] is the web's `sm` size: the value alone, the label only
 * read out. A chosen value has a clear button, named [clearLabel], unless [onClear] is null.
 */
@Composable
fun <T> VSelect(
    label: String,
    options: List<Pair<T, String>>,
    selected: T?,
    onSelect: (T) -> Unit,
    modifier: Modifier = Modifier,
    clearLabel: String? = null,
    onClear: (() -> Unit)? = null,
    placeholder: String = label,
    enabled: Boolean = true,
    compact: Boolean = false,
) {
    var open by remember { mutableStateOf(false) }
    val colors = MaterialTheme.colorScheme
    val value = options.firstOrNull { it.first == selected }?.second
    Box(modifier) {
        Row(
            Modifier
                .fillMaxWidth()
                .clip(VoltisShapes.field)
                .background(VoltisTheme.colors.field)
                .clickable(enabled, role = Role.DropdownList) { open = true }
                .semantics {
                    contentDescription = label
                    stateDescription = value ?: placeholder
                }
                .heightIn(min = 48.dp)
                .padding(start = 16.dp, end = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            // A value has the label over it, as VTextField's floating label.
            Column(Modifier.weight(1f).padding(vertical = 4.dp).clearAndSetSemantics {}) {
                if (value != null && !compact) {
                    Text(label, color = colors.onSurfaceVariant, style = MaterialTheme.typography.bodySmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                Text(
                    value ?: placeholder,
                    color = if (value == null) colors.onSurfaceVariant else colors.onSurface,
                    style = MaterialTheme.typography.bodyLarge,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            if (value != null && clearLabel != null && onClear != null) VIconButton(VIcons.Close, clearLabel, onClear, small = true, enabled = enabled)
            Icon(VIcons.ChevronDown, contentDescription = null, tint = colors.onSurfaceVariant)
        }
        VMenu(open, { open = false }) {
            for ((option, name) in options) {
                DropdownMenuItem(
                    text = { Text(name) },
                    onClick = {
                        open = false
                        if (option != selected) onSelect(option)
                    },
                    trailingIcon = if (option == selected) ({ Icon(VIcons.Check, contentDescription = null, tint = colors.primary) }) else null,
                )
            }
        }
    }
}
