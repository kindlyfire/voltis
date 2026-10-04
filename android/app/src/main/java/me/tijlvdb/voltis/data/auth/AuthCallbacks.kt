package me.tijlvdb.voltis.data.auth

import android.net.Uri
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.ui.UiText

/** Receives `voltis://auth/callback` and finishes the browser sign-in. */
@Singleton
class AuthCallbacks @Inject constructor(
    private val auth: AuthRepository,
    private val store: SessionStore,
    @param:AppScope private val scope: CoroutineScope,
) {
    sealed interface Status {
        data object Idle : Status

        data object Working : Status

        data class Failed(val text: UiText) : Status
    }

    private val _status = MutableStateFlow<Status>(Status.Idle)
    val status: StateFlow<Status> = _status.asStateFlow()

    fun handle(uri: Uri?) {
        if (uri == null || uri.scheme != "voltis" || uri.host != "auth" || uri.path != "/callback") return
        // Never show query text: any web page can open this URL.
        val code = uri.getQueryParameter("code")
        scope.launch {
            if (code == null) {
                attempt { auth.cancelBrowserSignIn() }
                return@launch
            }
            _status.value = Status.Working
            val failure = attempt { auth.completeBrowserSignIn(code) }
            // Only Login shows a failure; kept elsewhere, it would turn up stale at the next sign-out.
            val onLogin = store.state.first { it != SessionState.Loading }
                .let { it is SessionState.SignedOut || it is SessionState.NeedsReauth }
            _status.value = if (failure != null && onLogin) Status.Failed(failure) else Status.Idle
        }
    }

    fun dismiss() {
        _status.value = Status.Idle
    }
}
