package me.tijlvdb.voltis.ui.server

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.input.TextFieldLineLimits
import androidx.compose.material3.Button
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.ServerUrl
import me.tijlvdb.voltis.ui.ErrorText
import me.tijlvdb.voltis.ui.FormScreen
import me.tijlvdb.voltis.ui.rememberLocalNetworkAction

@Composable
fun ServerScreen(vm: ServerViewModel = hiltViewModel()) {
    val connect = rememberLocalNetworkAction(vm::connect)
    val submit = { connect(ServerUrl.normalize(vm.url.text.toString())) }
    FormScreen(stringResource(R.string.server_title)) {
        OutlinedTextField(
            state = vm.url,
            label = { Text(stringResource(R.string.server_url)) },
            supportingText = if (vm.insecure) {
                { Text(stringResource(R.string.server_insecure)) }
            } else {
                null
            },
            lineLimits = TextFieldLineLimits.SingleLine,
            keyboardOptions = KeyboardOptions(
                keyboardType = KeyboardType.Uri,
                autoCorrectEnabled = false,
                imeAction = ImeAction.Go,
            ),
            onKeyboardAction = { submit() },
            enabled = !vm.busy,
            modifier = Modifier.fillMaxWidth(),
        )
        ErrorText(vm.error)
        Button(onClick = submit, enabled = !vm.busy, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.server_connect))
        }
    }
}
