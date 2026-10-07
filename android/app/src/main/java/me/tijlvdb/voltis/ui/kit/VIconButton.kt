package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.FilledTonalIconButton
import androidx.compose.material3.FilledTonalIconToggleButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.IconToggleButton
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/**
 * [Subtle] is the tonal button of a row header: `surface-2` under a muted icon. [Floating] is a raised
 * circle for buttons over content, such as a page.
 */
enum class VIconButtonStyle { Plain, Tonal, Subtle, Floating }

/** An icon button named by [label]. [small] shrinks the circle to 32 dp; the touch target stays 48 dp. */
@Composable
fun VIconButton(
    icon: Painter,
    label: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    style: VIconButtonStyle = VIconButtonStyle.Plain,
    small: Boolean = false,
    enabled: Boolean = true,
) {
    val scheme = MaterialTheme.colorScheme
    val (container, content) = when (style) {
        VIconButtonStyle.Plain -> Color.Transparent to scheme.onSurface
        // The web's neutral tonal button: `surface-4` under `fg`.
        VIconButtonStyle.Tonal -> scheme.surfaceContainerHighest to scheme.onSurface
        VIconButtonStyle.Subtle -> scheme.surfaceContainer to scheme.onSurfaceVariant
        VIconButtonStyle.Floating -> scheme.surfaceContainerHigh to scheme.onSurface
    }
    when {
        style == VIconButtonStyle.Floating -> Box(
            modifier.size(48.dp).shadow(6.dp, CircleShape).background(container, CircleShape).clip(CircleShape)
                .clickable(enabled, role = Role.Button, onClick = onClick),
            Alignment.Center,
        ) { Icon(icon, label, tint = content) }
        small -> Box(
            modifier.size(48.dp).clip(CircleShape).clickable(enabled, role = Role.Button, onClick = onClick),
            Alignment.Center,
        ) {
            Box(Modifier.size(32.dp).background(container, CircleShape), Alignment.Center) {
                Icon(icon, label, Modifier.size(20.dp), tint = content)
            }
        }
        style == VIconButtonStyle.Plain -> IconButton(onClick, modifier, enabled) { Icon(icon, label) }
        else -> FilledTonalIconButton(
            onClick,
            modifier,
            enabled,
            colors = IconButtonDefaults.filledTonalIconButtonColors(containerColor = container, contentColor = content),
        ) { Icon(icon, label) }
    }
}

/** The star toggle named by [label]: filled, in the star's colour, while [checked]. [tonal] gives it the tonal button's circle. */
@Composable
fun VStarToggle(label: String, checked: Boolean, onChange: (Boolean) -> Unit, tonal: Boolean = false, enabled: Boolean = true) {
    val scheme = MaterialTheme.colorScheme
    val checkedTint = VoltisTheme.colors.star
    val content: @Composable () -> Unit = { Icon(if (checked) VIcons.StarFilled else VIcons.Star, label) }
    if (tonal) {
        val colors = IconButtonDefaults.filledTonalIconToggleButtonColors(
            containerColor = scheme.surfaceContainerHighest,
            contentColor = scheme.onSurface,
            checkedContainerColor = scheme.secondaryContainer,
            checkedContentColor = checkedTint,
        )
        FilledTonalIconToggleButton(checked, onChange, enabled = enabled, colors = colors, content = content)
    } else {
        VIconToggle(VIcons.Star, VIcons.StarFilled, label, checked, onChange, enabled = enabled, checkedColor = checkedTint)
    }
}

/** A toggle named by [label], showing [checkedIcon] while [checked]: TalkBack says its state. */
@Composable
fun VIconToggle(
    icon: Painter,
    checkedIcon: Painter,
    label: String,
    checked: Boolean,
    onChange: (Boolean) -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    checkedColor: Color = MaterialTheme.colorScheme.primary,
) {
    val colors = IconButtonDefaults.iconToggleButtonColors(contentColor = LocalContentColor.current, checkedContentColor = checkedColor)
    IconToggleButton(checked, onChange, modifier, enabled, colors = colors) { Icon(if (checked) checkedIcon else icon, label) }
}
