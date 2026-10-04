package me.tijlvdb.voltis.ui.home

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.SessionMethod
import me.tijlvdb.voltis.ui.FormScreen
import me.tijlvdb.voltis.ui.Loaded
import me.tijlvdb.voltis.ui.rememberLocalNetworkAction

@Composable
fun HomeScreen(vm: HomeViewModel = hiltViewModel()) {
    val session by vm.session.collectAsStateWithLifecycle()
    val server = session.server
    val load = rememberLocalNetworkAction(vm::load)
    FormScreen(stringResource(R.string.app_name), server?.url?.host) {
        Loaded(vm.me, vm.error, retry = { load(server?.url) }) { me ->
            Text(stringResource(R.string.home_signed_in_as, me.username), style = MaterialTheme.typography.titleMedium)
            Text(stringResource(R.string.home_method, methodLabel(me.sessionMethod)))
        }
        OutlinedButton(onClick = vm::signOut, enabled = !vm.signingOut, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.home_sign_out))
        }
    }
}

@Composable
private fun methodLabel(method: String) = when (method) {
    SessionMethod.PASSWORD -> stringResource(R.string.method_password)
    SessionMethod.OIDC -> stringResource(R.string.method_oidc)
    SessionMethod.PROXY -> stringResource(R.string.method_proxy)
    else -> method
}
