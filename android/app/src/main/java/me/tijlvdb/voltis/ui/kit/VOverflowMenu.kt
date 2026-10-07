package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.MenuDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.ui.theme.VoltisShapes

/** A dropdown menu in the kit's shape and colour. */
@Composable
internal fun VMenu(open: Boolean, onDismiss: () -> Unit, content: @Composable ColumnScope.() -> Unit) {
    DropdownMenu(open, onDismiss, shape = VoltisShapes.menu, containerColor = MaterialTheme.colorScheme.surfaceContainer, content = content)
}

/** A menu item: [danger] for a destructive one. */
data class VMenuItem(val label: String, val enabled: Boolean = true, val danger: Boolean = false, val onClick: () -> Unit)

/** The "More options" button with its menu of [items]: each a label and what choosing it does. */
@Composable
fun VOverflowMenu(items: List<Pair<String, () -> Unit>>, style: VIconButtonStyle = VIconButtonStyle.Plain, enabled: Boolean = true) {
    VOverflowMenu(items.map { (label, action) -> VMenuItem(label, onClick = action) }, style, enabled, label = null)
}

/** The same with [VMenuItem]s; [label] names the button in place of "More options". */
@Composable
fun VOverflowMenu(items: List<VMenuItem>, style: VIconButtonStyle = VIconButtonStyle.Plain, enabled: Boolean = true, label: String? = null) {
    Box {
        var open by remember { mutableStateOf(false) }
        VIconButton(VIcons.DotsVertical, label ?: stringResource(R.string.more_options), { open = true }, style = style, enabled = enabled)
        VMenu(open, { open = false }) {
            for (item in items) {
                val color = if (item.danger) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface
                DropdownMenuItem(
                    { Text(item.label) },
                    {
                        open = false
                        item.onClick()
                    },
                    enabled = item.enabled,
                    colors = MenuDefaults.itemColors(textColor = color),
                )
            }
        }
    }
}
