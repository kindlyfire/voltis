package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.ui.isLarge

/** The page's title, as the web's `APageHeader`: larger on a large window. */
@Composable
fun PageHeader(title: String, modifier: Modifier = Modifier) {
    Text(title, modifier.padding(top = 8.dp).semantics { heading() }, style = pageTitleStyle())
}

/** The style of a page's title. */
@Composable
fun pageTitleStyle(): TextStyle = if (isLarge()) MaterialTheme.typography.displayMedium else MaterialTheme.typography.displaySmall
