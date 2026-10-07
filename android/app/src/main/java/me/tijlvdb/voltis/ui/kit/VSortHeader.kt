package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R

enum class SortOrder { Ascending, Descending }

/**
 * A sortable column's header, as the web's `ASortHeader`: a muted label, then the arrow of
 * [order] in the text colour while the list is sorted by it. The arrow keeps its room while
 * unsorted, so nothing moves.
 */
@Composable
fun VSortHeader(text: String, order: SortOrder?, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val scheme = MaterialTheme.colorScheme
    val state = order?.let { stringResource(if (it == SortOrder.Ascending) R.string.sorted_ascending else R.string.sorted_descending) }
    TextButton(
        onClick,
        modifier.semantics { if (state != null) stateDescription = state },
        shape = CircleShape,
        colors = ButtonDefaults.textButtonColors(contentColor = scheme.onSurfaceVariant),
    ) {
        Text(text)
        Spacer(Modifier.width(4.dp))
        Icon(
            if (order == SortOrder.Ascending) VIcons.SortAscending else VIcons.SortDescending,
            contentDescription = null,
            Modifier.size(16.dp),
            tint = if (order == null) Color.Transparent else scheme.onSurface,
        )
    }
}
