package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.ui.theme.VoltisShapes

private val MIN_WIDTH = 88.dp

/**
 * An icon over its [label], for a bar of actions. A long label wraps rather than clips. Disabled, a tap says [reason].
 */
@Composable
fun VBarButton(icon: Painter, label: String, onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true, reason: String? = null) {
    val color = MaterialTheme.colorScheme.onSurface.let { if (enabled) it else it.copy(alpha = 0.38f) }
    // A tap on the disabled button says why (VDisabled), which is also its one labelled TalkBack stop. No clickable while disabled: it would take the tap.
    VDisabled(if (enabled) null else reason ?: label, label, modifier.clip(VoltisShapes.menuItem)) { on ->
        Column(
            Modifier
                .then(if (on) Modifier.clickable(role = Role.Button, onClick = onClick) else Modifier)
                .defaultMinSize(minWidth = MIN_WIDTH, minHeight = 56.dp)
                .padding(horizontal = 12.dp, vertical = 6.dp),
            Arrangement.spacedBy(2.dp, Alignment.CenterVertically),
            Alignment.CenterHorizontally,
        ) {
            Icon(icon, contentDescription = null, tint = color)
            Text(label, color = color, style = MaterialTheme.typography.labelMedium, textAlign = TextAlign.Center)
        }
    }
}
