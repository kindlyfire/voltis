package me.tijlvdb.voltis.ui.login

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.input.TextFieldLineLimits
import androidx.compose.material3.Button
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedSecureTextField
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.autofill.ContentType
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.contentType
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Info
import me.tijlvdb.voltis.data.auth.AuthCallbacks
import me.tijlvdb.voltis.data.auth.SessionState
import me.tijlvdb.voltis.ui.ErrorText
import me.tijlvdb.voltis.ui.FormScreen
import me.tijlvdb.voltis.ui.Loaded
import me.tijlvdb.voltis.ui.rememberLocalNetworkAction
import me.tijlvdb.voltis.ui.openInBrowser

@Composable
fun LoginScreen(vm: LoginViewModel = hiltViewModel()) {
    val session by vm.session.collectAsStateWithLifecycle()
    val callback by vm.callbackStatus.collectAsStateWithLifecycle()
    val server = session.server
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val enabled = !vm.busy && callback != AuthCallbacks.Status.Working
    val loadInfo = rememberLocalNetworkAction(vm::loadInfo)
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { vm.loadInfo() }

    FormScreen(stringResource(R.string.login_title), server?.url?.host) {
        if (session is SessionState.NeedsReauth) Text(stringResource(R.string.login_reauth))

        when (val c = callback) {
            AuthCallbacks.Status.Working -> {
                val label = stringResource(R.string.login_signing_in)
                LinearProgressIndicator(Modifier.fillMaxWidth().semantics { contentDescription = label })
            }
            is AuthCallbacks.Status.Failed -> ErrorText(c.text)
            AuthCallbacks.Status.Idle -> Unit
        }

        Loaded(vm.info, vm.infoError, retry = { loadInfo(server?.url) }) { info ->
            if (info.firstUserFlow) {
                Text(stringResource(R.string.login_setup_needed))
                Button(
                    onClick = { if (server?.let { context.openInBrowser(it.url.toString()) } == false) vm.browserFailed() },
                    enabled = enabled,
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Text(stringResource(R.string.login_finish_setup))
                }
            } else {
                SignInForm(vm, info, enabled) {
                    scope.launch {
                        val url = vm.browserSignInUrl() ?: return@launch
                        if (!context.openInBrowser(url)) vm.browserFailed()
                    }
                }
            }
        }

        ErrorText(vm.error)
        TextButton(onClick = vm::changeServer, enabled = enabled) {
            Text(stringResource(R.string.login_change_server))
        }
    }
}

@Composable
private fun SignInForm(vm: LoginViewModel, info: Info, enabled: Boolean, onBrowser: () -> Unit) {
    if (info.passwordLoginEnabled) {
        OutlinedTextField(
            state = vm.username,
            label = { Text(stringResource(R.string.login_username)) },
            lineLimits = TextFieldLineLimits.SingleLine,
            keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, imeAction = ImeAction.Next),
            enabled = enabled,
            modifier = Modifier.fillMaxWidth().semantics { contentType = ContentType.Username },
        )
        OutlinedSecureTextField(
            state = vm.password,
            label = { Text(stringResource(R.string.login_password)) },
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
            onKeyboardAction = { vm.signIn() },
            enabled = enabled,
            modifier = Modifier.fillMaxWidth().semantics { contentType = ContentType.Password },
        )
        Button(onClick = vm::signIn, enabled = enabled, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.login_submit))
        }
    }
    // The browser covers every web login (SSO, a login proxy, password), so it's always offered.
    val label = if (info.oidcEnabled) info.oidcButtonLabel else stringResource(R.string.login_browser)
    if (info.oidcEnabled || !info.passwordLoginEnabled) {
        Button(onClick = onBrowser, enabled = enabled, modifier = Modifier.fillMaxWidth()) { Text(label) }
    } else {
        OutlinedButton(onClick = onBrowser, enabled = enabled, modifier = Modifier.fillMaxWidth()) { Text(label) }
    }
}
