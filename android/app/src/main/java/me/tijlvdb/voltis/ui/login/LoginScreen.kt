package me.tijlvdb.voltis.ui.login

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Text
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
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Info
import me.tijlvdb.voltis.data.auth.AuthCallbacks
import me.tijlvdb.voltis.data.auth.SessionState
import me.tijlvdb.voltis.ui.Loaded
import me.tijlvdb.voltis.ui.SignInScreen
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VDialog
import me.tijlvdb.voltis.ui.kit.VTextField
import me.tijlvdb.voltis.ui.openInBrowser
import me.tijlvdb.voltis.ui.rememberLocalNetworkAction

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

    SignInScreen(stringResource(R.string.login_title), server?.url?.host) {
        if (session is SessionState.NeedsReauth) Text(stringResource(R.string.login_reauth))

        when (val c = callback) {
            AuthCallbacks.Status.Working -> {
                val label = stringResource(R.string.login_signing_in)
                LinearProgressIndicator(Modifier.fillMaxWidth().semantics { contentDescription = label })
            }
            is AuthCallbacks.Status.Failed -> QueryError(c.text)
            AuthCallbacks.Status.Idle -> Unit
        }

        Loaded(vm.info, vm.infoError, retry = { loadInfo(server?.url) }) { info ->
            if (info.firstUserFlow) {
                Text(stringResource(R.string.login_setup_needed))
                VButton(
                    stringResource(R.string.login_finish_setup),
                    onClick = { if (server?.let { context.openInBrowser(it.url.toString()) } == false) vm.browserFailed() },
                    modifier = Modifier.fillMaxWidth(),
                    enabled = enabled,
                )
            } else {
                SignInForm(vm, info, enabled) {
                    scope.launch {
                        val url = vm.browserSignInUrl() ?: return@launch
                        if (!context.openInBrowser(url)) vm.browserFailed()
                    }
                }
            }
        }

        QueryError(vm.error)
        // A text button's label is 12 dp inside it: it lines up with the card's content.
        VButton(stringResource(R.string.login_change_server), vm::changeServer, Modifier.offset(x = (-12).dp), VButtonStyle.Text, enabled)
    }
    if (vm.confirmingChange) {
        VDialog(
            stringResource(R.string.login_change_server_confirm),
            stringResource(R.string.login_change_server),
            onConfirm = { vm.confirmChange(true) },
            onDismiss = { vm.confirmChange(false) },
            text = stringResource(R.string.login_change_server_text),
        )
    }
}

@Composable
private fun SignInForm(vm: LoginViewModel, info: Info, enabled: Boolean, onBrowser: () -> Unit) {
    if (info.passwordLoginEnabled) {
        VTextField(
            state = vm.username,
            label = stringResource(R.string.login_username),
            keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, imeAction = ImeAction.Next),
            enabled = enabled,
            modifier = Modifier.fillMaxWidth().semantics { contentType = ContentType.Username },
        )
        VTextField(
            state = vm.password,
            label = stringResource(R.string.login_password),
            secure = true,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
            onKeyboardAction = { vm.signIn() },
            enabled = enabled,
            modifier = Modifier.fillMaxWidth().semantics { contentType = ContentType.Password },
        )
        VButton(stringResource(R.string.login_submit), vm::signIn, Modifier.fillMaxWidth(), enabled = enabled)
    }
    // The browser covers every web login (SSO, a login proxy, password), so it's always offered.
    val label = if (info.oidcEnabled) info.oidcButtonLabel else stringResource(R.string.login_browser)
    val style = if (info.oidcEnabled || !info.passwordLoginEnabled) VButtonStyle.Filled else VButtonStyle.Tonal
    VButton(label, onBrowser, Modifier.fillMaxWidth(), style, enabled)
}
