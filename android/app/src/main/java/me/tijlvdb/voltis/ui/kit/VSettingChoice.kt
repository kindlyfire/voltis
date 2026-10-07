package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R

/** A labelled segmented control, with what Auto resolves to under it while Auto is selected. */
@Composable
fun <T> VSettingChoice(label: String, options: List<Pair<T, String>>, selected: T, onSelect: (T) -> Unit, auto: String? = null) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        // The control below is named after it.
        Text(label, Modifier.clearAndSetSemantics {}, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
        VSegmented(label, options, selected, onSelect)
        if (auto != null) {
            Text(
                stringResource(R.string.reader_auto_is, auto),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodySmall,
            )
        }
    }
}
