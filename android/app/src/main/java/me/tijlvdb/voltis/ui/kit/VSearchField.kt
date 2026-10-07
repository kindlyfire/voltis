package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.input.TextFieldLineLimits
import androidx.compose.foundation.text.input.TextFieldState
import androidx.compose.foundation.text.input.clearText
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/**
 * The web's search pill, named [label]. [placeholder] shows while it is empty; [busy] swaps the
 * magnifier for a spinner.
 */
@Composable
fun VSearchField(state: TextFieldState, label: String, placeholder: String, modifier: Modifier = Modifier, busy: Boolean = false) {
    val colors = MaterialTheme.colorScheme
    val keyboard = LocalSoftwareKeyboardController.current
    BasicTextField(
        state,
        modifier.fillMaxWidth().semantics { contentDescription = label },
        textStyle = MaterialTheme.typography.bodyLarge.copy(color = colors.onSurface),
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
        onKeyboardAction = { keyboard?.hide() },
        lineLimits = TextFieldLineLimits.SingleLine,
        cursorBrush = SolidColor(colors.primary),
        decorator = { field ->
            Row(
                Modifier.height(48.dp).background(VoltisTheme.colors.search, CircleShape).padding(start = 16.dp, end = if (state.text.isEmpty()) 16.dp else 0.dp),
                Arrangement.spacedBy(12.dp),
                Alignment.CenterVertically,
            ) {
                if (busy) {
                    CircularProgressIndicator(Modifier.padding(1.dp).size(22.dp), strokeWidth = 2.dp)
                } else {
                    Icon(VIcons.Magnify, contentDescription = null, tint = colors.onSurfaceVariant)
                }
                Box(Modifier.weight(1f)) {
                    if (state.text.isEmpty()) Text(placeholder, color = colors.onSurfaceVariant, style = MaterialTheme.typography.bodyLarge)
                    field()
                }
                if (state.text.isNotEmpty()) {
                    VIconButton(VIcons.Close, stringResource(R.string.search_clear), { state.clearText() }, small = true)
                }
            }
        },
    )
}
