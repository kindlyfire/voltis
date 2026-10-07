package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

enum class VButtonStyle { Filled, Tonal, Text }

/**
 * A pill button; Material adds the 48 dp touch target around its 40 dp height. [icon] leads the
 * text. [danger] is the filled or text button of a destructive or failed action. [description]
 * is what a screen reader says instead of the text, for a button that acts on a named item.
 */
@Composable
fun VButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    style: VButtonStyle = VButtonStyle.Filled,
    enabled: Boolean = true,
    icon: Painter? = null,
    danger: Boolean = false,
    description: String? = null,
    /** One line, ellipsized: a label of any length keeps the button's height. */
    singleLine: Boolean = false,
) {
    val scheme = MaterialTheme.colorScheme
    val label: @Composable RowScope.() -> Unit = {
        if (icon != null) {
            Icon(icon, contentDescription = null, Modifier.size(20.dp))
            Spacer(Modifier.width(8.dp))
        }
        Text(text, maxLines = if (singleLine) 1 else Int.MAX_VALUE, overflow = if (singleLine) TextOverflow.Ellipsis else TextOverflow.Clip)
    }
    val modifier = if (description == null) modifier else modifier.semantics { contentDescription = description }
    when (style) {
        VButtonStyle.Filled -> {
            val colors = if (danger) ButtonDefaults.buttonColors(scheme.error, scheme.onError) else ButtonDefaults.buttonColors()
            Button(onClick, modifier, enabled, CircleShape, colors, content = label)
        }
        VButtonStyle.Tonal -> FilledTonalButton(onClick, modifier, enabled, CircleShape, content = label)
        VButtonStyle.Text -> {
            val colors = if (danger) ButtonDefaults.textButtonColors(contentColor = scheme.error) else ButtonDefaults.textButtonColors()
            TextButton(onClick, modifier, enabled, CircleShape, colors, content = label)
        }
    }
}
