package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.unit.dp

/**
 * A thin bar filled to [fraction] (0 to 1), 4 dp unless [modifier] sets a height. [overlay] is the
 * square bar with a dark translucent track, for covers. [reversed] fills from the right edge.
 * The caller gives it its semantics.
 */
@Composable
fun VProgressBar(fraction: Float, modifier: Modifier = Modifier, overlay: Boolean = false, reversed: Boolean = false) {
    val colors = MaterialTheme.colorScheme
    val fill = colors.primary
    Spacer(
        modifier
            .height(4.dp)
            .clip(if (overlay) RectangleShape else CircleShape)
            .background(if (overlay) Color.Black.copy(alpha = 0.22f) else colors.primaryContainer)
            .drawBehind {
                val width = size.width * fraction.coerceIn(0f, 1f)
                drawRect(fill, Offset(if (reversed) size.width - width else 0f, 0f), Size(width, size.height))
            },
    )
}
