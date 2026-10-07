package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.theme.VoltisShapes
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/**
 * A dialog that asks before acting: [text] and [content], then Cancel and [confirm]. While
 * [busy] it can't be dismissed, unless [dismissable] says so (its owner then reports the action's
 * outcome elsewhere). A failed action leaves it open, showing [error].
 */
@Composable
fun VDialog(
    title: String,
    confirm: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
    text: String? = null,
    confirmEnabled: Boolean = true,
    danger: Boolean = false,
    busy: Boolean = false,
    error: UiText? = null,
    dismissable: Boolean = !busy,
    /** The dismiss button's text; null for a dialog with none. */
    dismissLabel: String? = stringResource(R.string.cancel),
    content: (@Composable ColumnScope.() -> Unit)? = null,
) {
    val body: (@Composable () -> Unit)? = if (text == null && content == null && error == null) null else ({
        // Material mutes a dialog's text; the web's is the foreground color.
        CompositionLocalProvider(LocalContentColor provides MaterialTheme.colorScheme.onSurface) {
            Column(Modifier.verticalScroll(rememberScrollState()), Arrangement.spacedBy(12.dp)) {
                if (text != null) Text(text)
                content?.invoke(this)
                QueryError(error)
            }
        }
    })
    AlertDialog(
        onDismissRequest = { if (dismissable) onDismiss() },
        confirmButton = { VButton(confirm, onConfirm, enabled = confirmEnabled && !busy, danger = danger) },
        dismissButton = dismissLabel?.let { { VButton(it, onDismiss, style = VButtonStyle.Text, enabled = dismissable) } },
        title = { Text(title, style = MaterialTheme.typography.titleLarge) },
        text = body,
        shape = VoltisShapes.dialog,
        containerColor = VoltisTheme.colors.raised,
    )
}

/**
 * A dialog whose buttons are its choices, without Cancel: dismissing it is [onDismiss]. [buttons]
 * are laid out by [VDialogButtons]; the main one, last, takes the modifier they are given, which
 * focuses it when the dialog opens.
 */
@Composable
fun VChoiceDialog(title: String, text: String, onDismiss: () -> Unit, buttons: @Composable (main: Modifier) -> Unit) {
    val main = remember { FocusRequester() }
    AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = {
            VDialogButtons { buttons(Modifier.focusRequester(main)) }
            // In the dialog's own composition: the requester is attached by then.
            LaunchedEffect(main) { main.requestFocus() }
        },
        title = { Text(title, style = MaterialTheme.typography.titleLarge) },
        text = {
            Text(text, Modifier.verticalScroll(rememberScrollState()), color = MaterialTheme.colorScheme.onSurface)
        },
        shape = VoltisShapes.dialog,
        containerColor = VoltisTheme.colors.raised,
    )
}

/**
 * A dialog's buttons, the main one last: in one row at the end when they fit, else stacked at full
 * width with the main one on top. Material would wrap them into ragged lines. Keyboard focus moves
 * in the order they are placed, so stacked it starts at the top too.
 */
@Composable
fun VDialogButtons(buttons: @Composable () -> Unit) {
    Layout(buttons) { measurables, constraints ->
        val width = constraints.maxWidth
        val fits = measurables.sumOf { it.maxIntrinsicWidth(constraints.maxHeight) } + 8.dp.roundToPx() * (measurables.size - 1) <= width
        // Stacked, the buttons' 48 dp touch targets already space them by 8 dp.
        val gap = if (fits) 8.dp.roundToPx() else 0
        val placeables = if (fits) {
            measurables.map { it.measure(constraints.copy(minWidth = 0, minHeight = 0)) }
        } else {
            measurables.map { it.measure(Constraints.fixedWidth(width)) }.reversed()
        }
        val height = if (fits) placeables.maxOf { it.height } else placeables.sumOf { it.height } + gap * (placeables.size - 1)
        layout(width, height) {
            var at = if (fits) width - placeables.sumOf { it.width } - gap * (placeables.size - 1) else 0
            for (p in placeables) {
                if (fits) {
                    p.placeRelative(at, (height - p.height) / 2)
                    at += p.width + gap
                } else {
                    p.placeRelative(0, at)
                    at += p.height + gap
                }
            }
        }
    }
}
