package me.tijlvdb.voltis.ui.theme

import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Shapes
import androidx.compose.ui.unit.dp

/** The `--radius-*` tokens of the web. Buttons, chips and badges are pills (`CircleShape`). */
object VoltisShapes {
    val field = RoundedCornerShape(12.dp)
    val card = RoundedCornerShape(18.dp)
    val menu = RoundedCornerShape(14.dp)
    val menuItem = RoundedCornerShape(8.dp)
    val dialog = RoundedCornerShape(22.dp)
    val cover = RoundedCornerShape(12.dp)
    val toast = RoundedCornerShape(12.dp)
}

/** Set so Material components land close by default. */
val MaterialShapes = Shapes(
    extraSmall = VoltisShapes.menuItem,
    small = VoltisShapes.field,
    medium = VoltisShapes.card,
    large = VoltisShapes.card,
    extraLarge = VoltisShapes.dialog,
)
