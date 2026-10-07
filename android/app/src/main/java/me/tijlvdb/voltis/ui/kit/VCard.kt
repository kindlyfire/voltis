package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.ui.theme.VoltisShapes
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/** The web's raised card: a titled section on `raised`, outlined by the card line. */
@Composable
fun VCard(title: String, modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    val colors = VoltisTheme.colors
    Column(
        modifier
            .fillMaxWidth()
            .background(colors.raised, VoltisShapes.card)
            .border(1.dp, colors.cardLine, VoltisShapes.card)
            .padding(horizontal = 22.dp, vertical = 20.dp),
        Arrangement.spacedBy(12.dp),
    ) {
        Text(title, Modifier.semantics { heading() }, style = MaterialTheme.typography.titleMedium)
        content()
    }
}

/** The same card as one tappable row (a cover beside its text), read as one button. */
@Composable
fun VCard(onClick: () -> Unit, modifier: Modifier = Modifier, content: @Composable RowScope.() -> Unit) {
    val colors = VoltisTheme.colors
    Row(
        modifier
            .fillMaxWidth()
            .clip(VoltisShapes.card)
            .background(colors.raised)
            .border(1.dp, colors.cardLine, VoltisShapes.card)
            .clickable(role = Role.Button, onClick = onClick)
            .padding(12.dp),
        Arrangement.spacedBy(16.dp),
        content = content,
    )
}
