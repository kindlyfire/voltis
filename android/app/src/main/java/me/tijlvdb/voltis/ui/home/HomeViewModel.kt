package me.tijlvdb.voltis.ui.home

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.api.Me
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.auth.AuthRepository
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.ui.UiText

@HiltViewModel
class HomeViewModel @Inject constructor(
    private val api: VoltisApi,
    private val auth: AuthRepository,
    store: SessionStore,
) : ViewModel() {
    val session = store.state

    var me by mutableStateOf<Me?>(null)
        private set

    var error by mutableStateOf<UiText?>(null)
        private set

    var signingOut by mutableStateOf(false)
        private set

    init {
        load()
    }

    fun load() {
        error = null
        viewModelScope.launch { error = attempt { me = api.me() } }
    }

    fun signOut() {
        if (signingOut) return
        signingOut = true
        viewModelScope.launch {
            // On success the session state changes and AppNav leaves this screen.
            error = attempt { auth.logout() }
            signingOut = false
        }
    }
}
