package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.AssistChip
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FilterChipDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/** A pill that is on or off, checked while [selected]. Material adds the 48 dp touch target. */
@Composable
fun VChip(text: String, selected: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true) {
    val scheme = MaterialTheme.colorScheme
    FilterChip(
        selected,
        onClick,
        label = { Text(text) },
        modifier = modifier,
        enabled = enabled,
        leadingIcon = if (selected) ({ Icon(VIcons.Check, contentDescription = null, Modifier.size(18.dp)) }) else null,
        shape = CircleShape,
        colors = FilterChipDefaults.filterChipColors(
            selectedContainerColor = scheme.secondaryContainer,
            selectedLabelColor = scheme.onSecondaryContainer,
            selectedLeadingIconColor = scheme.onSecondaryContainer,
        ),
    )
}

/** The web chip's tones. */
enum class VTone { Neutral, Primary, Success, Warning }

/**
 * A small pill that only says something: a type, a count, a status. [VTone.Primary] is the tone of a "2 new" count.
 * [onCard] is for a neutral tag on a [VCard], whose dark `raised` is the tag's own surface.
 */
@Composable
fun VTag(text: String, modifier: Modifier = Modifier, tone: VTone = VTone.Neutral, onCard: Boolean = false) {
    val scheme = MaterialTheme.colorScheme
    val colors = VoltisTheme.colors
    val (container, content) = when (tone) {
        VTone.Neutral -> (if (onCard) colors.field else scheme.surfaceContainerHighest) to scheme.onSurface
        VTone.Primary -> scheme.primaryContainer to scheme.onPrimaryContainer
        VTone.Success -> colors.successContainer to colors.onSuccessContainer
        VTone.Warning -> colors.warningContainer to colors.onWarningContainer
    }
    Text(
        text,
        modifier.background(container, CircleShape).padding(horizontal = 10.dp, vertical = 4.dp),
        color = content,
        style = MaterialTheme.typography.labelMedium.copy(fontWeight = FontWeight.Medium),
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
    )
}

/** A pill that opens something, named by [text]. Material adds the 48 dp touch target. */
@Composable
fun VLinkChip(text: String, onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true) {
    AssistChip(onClick, label = { Text(text) }, modifier = modifier, enabled = enabled, shape = CircleShape)
}
