package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.unit.dp

// An inverse pill over a cover. Decorative: what it sits on names it.

@Composable
fun VBadge(text: String, modifier: Modifier = Modifier) {
    val colors = MaterialTheme.colorScheme
    Box(
        modifier.defaultMinSize(26.dp, 26.dp).background(colors.inverseSurface, CircleShape).padding(horizontal = 9.dp),
        Alignment.Center,
    ) {
        Text(text, color = colors.inverseOnSurface, style = MaterialTheme.typography.labelMedium, maxLines = 1)
    }
}

/** An icon dot. */
@Composable
fun VBadge(icon: Painter, modifier: Modifier = Modifier) {
    val colors = MaterialTheme.colorScheme
    Box(modifier.size(26.dp).background(colors.inverseSurface, CircleShape), Alignment.Center) {
        Icon(icon, contentDescription = null, Modifier.size(15.dp), tint = colors.inverseOnSurface)
    }
}
