package me.tijlvdb.voltis.ui.kit

import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import me.tijlvdb.voltis.R

/** A spinner that says what it waits for. */
@Composable
fun VSpinner(modifier: Modifier = Modifier, label: String = stringResource(R.string.loading)) {
    CircularProgressIndicator(modifier.semantics { contentDescription = label })
}
