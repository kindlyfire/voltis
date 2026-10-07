package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.BottomSheetDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp

/**
 * A modal bottom sheet under a heading, with [meta] or [action] at the heading's end. Its content
 * scrolls, and it opens whole: with a half-open state Back would take two presses. [lazy] is for
 * content that is a lazy list, which scrolls itself. [footer] is pinned under the content, always
 * in view.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun VSheet(
    title: String,
    onDismiss: () -> Unit,
    meta: String? = null,
    action: (@Composable () -> Unit)? = null,
    lazy: Boolean = false,
    footer: (@Composable () -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    val colors = MaterialTheme.colorScheme
    ModalBottomSheet(
        onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = colors.surfaceContainerLow,
        // The handle is a touch target (its actions collapse and dismiss), 32 dp wide by default.
        dragHandle = { Box(Modifier.widthIn(min = 48.dp), contentAlignment = Alignment.Center) { BottomSheetDefaults.DragHandle() } },
    ) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 24.dp).padding(bottom = 12.dp),
            Arrangement.SpaceBetween,
            Alignment.CenterVertically,
        ) {
            // The title takes what the end leaves; an action measured after a long title would get no width.
            Text(title, Modifier.weight(1f, fill = false).semantics { heading() }, style = MaterialTheme.typography.titleLarge)
            if (meta != null) Text(meta, color = colors.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
            action?.invoke()
        }
        if (lazy) {
            content()
        } else {
            Column(
                Modifier.weight(1f, fill = false).verticalScroll(rememberScrollState()).padding(horizontal = 24.dp).padding(bottom = if (footer == null) 16.dp else 8.dp),
                content = content,
            )
        }
        if (footer != null) Box(Modifier.fillMaxWidth().navigationBarsPadding().padding(start = 24.dp, end = 24.dp, top = 8.dp, bottom = 16.dp)) { footer() }
    }
}
