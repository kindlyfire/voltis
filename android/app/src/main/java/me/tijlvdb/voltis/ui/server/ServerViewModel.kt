package me.tijlvdb.voltis.ui.server

import androidx.compose.foundation.text.input.TextFieldState
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.text.TextRange
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.ServerUrl
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.auth.AuthRepository
import me.tijlvdb.voltis.ui.UiText

@HiltViewModel
class ServerViewModel @Inject constructor(private val auth: AuthRepository) : ViewModel() {
    val url = TextFieldState("https://", TextRange(8))

    val insecure by derivedStateOf { ServerUrl.normalize(url.text.toString())?.let(ServerUrl::isInsecure) == true }

    var busy by mutableStateOf(false)
        private set

    var error by mutableStateOf<UiText?>(null)
        private set

    fun connect() {
        if (busy) return
        val normalized = ServerUrl.normalize(url.text.toString())
        if (normalized == null) {
            error = UiText.Res(R.string.error_bad_url)
            return
        }
        busy = true
        error = null
        viewModelScope.launch {
            // On success the session state changes and AppNav moves on.
            error = attempt { auth.connect(normalized) }
            busy = false
        }
    }
}
