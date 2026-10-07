package me.tijlvdb.voltis.ui.login

import androidx.compose.foundation.text.input.TextFieldState
import androidx.compose.foundation.text.input.clearText
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Info
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.auth.AuthCallbacks
import me.tijlvdb.voltis.data.auth.AuthRepository
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.db.AccountStores
import me.tijlvdb.voltis.ui.UiText

@HiltViewModel
class LoginViewModel @Inject constructor(
    private val api: VoltisApi,
    private val auth: AuthRepository,
    private val callbacks: AuthCallbacks,
    private val stores: AccountStores,
    store: SessionStore,
) : ViewModel() {
    val session = store.state
    val callbackStatus = callbacks.status

    val username = TextFieldState()
    val password = TextFieldState()

    var info by mutableStateOf<Info?>(null)
        private set

    var infoError by mutableStateOf<UiText?>(null)
        private set

    var busy by mutableStateOf(false)
        private set

    var error by mutableStateOf<UiText?>(null)
        private set

    private var infoJob: Job? = null

    /** Called on every resume: setup or settings may have changed in the browser meanwhile. */
    fun loadInfo() {
        infoError = null
        infoJob?.cancel()
        // A failed refresh keeps the form that's already shown.
        infoJob = viewModelScope.launch { infoError = attempt { info = api.info() }.takeIf { info == null } }
    }

    fun signIn() = action {
        auth.passwordLogin(username.text.toString().trim(), password.text.toString())
        // This ViewModel can outlive the screen.
        password.clearText()
    }

    /** Persists a browser sign-in and returns the URL to open, or null if it couldn't start. */
    suspend fun browserSignInUrl(): String? {
        var url: String? = null
        act { url = auth.startBrowserSignIn() }
        return url
    }

    /** No browser could open a URL. */
    fun browserFailed() {
        error = UiText.Res(R.string.error_no_browser)
        viewModelScope.launch { attempt { auth.cancelBrowserSignIn() } }
    }

    /** This server's accounts left data on this device: Change server says it stays (P2 §11). */
    var confirmingChange by mutableStateOf(false)
        private set

    /** Asks first when this server's accounts left downloads or unsent changes here. */
    fun changeServer() = action {
        val server = session.value.server
        if (server != null && stores.dataOf(server.id).any) confirmingChange = true else auth.changeServer()
    }

    fun confirmChange(confirmed: Boolean) {
        confirmingChange = false
        if (confirmed) action { auth.changeServer() }
    }

    private fun action(block: suspend () -> Unit) {
        viewModelScope.launch { act(block) }
    }

    private suspend fun act(block: suspend () -> Unit) {
        if (busy) return
        busy = true
        error = null
        callbacks.dismiss() // a stale callback failure
        try {
            error = attempt(block)
        } finally {
            busy = false
        }
    }
}
