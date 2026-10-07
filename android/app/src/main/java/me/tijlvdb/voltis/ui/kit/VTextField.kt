package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.input.InputTransformation
import androidx.compose.foundation.text.input.KeyboardActionHandler
import androidx.compose.foundation.text.input.TextFieldLineLimits
import androidx.compose.foundation.text.input.TextFieldState
import androidx.compose.foundation.text.input.maxLength
import androidx.compose.material3.SecureTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextField
import androidx.compose.material3.TextFieldDefaults
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import me.tijlvdb.voltis.ui.theme.VoltisShapes
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/**
 * The web's filled field: a `field` box with the label inside. [secure] hides the text. [lines] above
 * one makes it multiline, that tall at least. [maxLength] rejects an edit that would exceed it, so a
 * longer paste is dropped whole rather than cut. [error] replaces [hint] and marks the field invalid.
 */
@Composable
fun VTextField(
    state: TextFieldState,
    label: String,
    modifier: Modifier = Modifier,
    hint: String? = null,
    secure: Boolean = false,
    enabled: Boolean = true,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
    onKeyboardAction: KeyboardActionHandler? = null,
    lines: Int = 1,
    maxLength: Int? = null,
    error: String? = null,
) {
    val field = VoltisTheme.colors.field
    val colors = TextFieldDefaults.colors(
        focusedContainerColor = field,
        unfocusedContainerColor = field,
        disabledContainerColor = field,
        focusedIndicatorColor = Color.Transparent,
        unfocusedIndicatorColor = Color.Transparent,
        disabledIndicatorColor = Color.Transparent,
        errorContainerColor = field,
        errorIndicatorColor = Color.Transparent,
    )
    val supporting: (@Composable () -> Unit)? = (error ?: hint)?.let { { Text(it) } }
    val limit = maxLength?.let { InputTransformation.maxLength(it) }
    if (secure) {
        SecureTextField(
            state = state,
            modifier = modifier,
            enabled = enabled,
            label = { Text(label) },
            supportingText = supporting,
            isError = error != null,
            inputTransformation = limit,
            keyboardOptions = keyboardOptions,
            onKeyboardAction = onKeyboardAction,
            shape = VoltisShapes.field,
            colors = colors,
        )
    } else {
        TextField(
            state = state,
            modifier = modifier,
            enabled = enabled,
            label = { Text(label) },
            supportingText = supporting,
            isError = error != null,
            inputTransformation = limit,
            keyboardOptions = keyboardOptions,
            onKeyboardAction = onKeyboardAction,
            lineLimits = if (lines > 1) TextFieldLineLimits.MultiLine(lines, lines * 2) else TextFieldLineLimits.SingleLine,
            shape = VoltisShapes.field,
            colors = colors,
        )
    }
}
