package me.tijlvdb.voltis.ui.server

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.ServerUrl
import me.tijlvdb.voltis.ui.SignInScreen
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VTextField
import me.tijlvdb.voltis.ui.rememberLocalNetworkAction

@Composable
fun ServerScreen(vm: ServerViewModel = hiltViewModel()) {
    val connect = rememberLocalNetworkAction(vm::connect)
    val submit = { connect(ServerUrl.normalize(vm.url.text.toString())) }
    SignInScreen(stringResource(R.string.server_title)) {
        VTextField(
            state = vm.url,
            label = stringResource(R.string.server_url),
            hint = if (vm.insecure) stringResource(R.string.server_insecure) else null,
            keyboardOptions = KeyboardOptions(
                keyboardType = KeyboardType.Uri,
                autoCorrectEnabled = false,
                imeAction = ImeAction.Go,
            ),
            onKeyboardAction = { submit() },
            enabled = !vm.busy,
            modifier = Modifier.fillMaxWidth(),
        )
        QueryError(vm.error)
        VButton(stringResource(R.string.server_connect), submit, Modifier.fillMaxWidth(), enabled = !vm.busy)
    }
}
