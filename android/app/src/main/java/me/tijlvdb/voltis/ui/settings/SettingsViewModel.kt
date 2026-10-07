package me.tijlvdb.voltis.ui.settings

import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.api.attemptResult
import me.tijlvdb.voltis.data.auth.AuthRepository
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.db.AccountData
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.data.settings.DeviceSettings
import me.tijlvdb.voltis.data.settings.ThemeMode
import me.tijlvdb.voltis.data.user.UserRepository
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.data.sync.SyncCenter
import me.tijlvdb.voltis.ui.Refresher
import me.tijlvdb.voltis.ui.UiText

@HiltViewModel
class SettingsViewModel @Inject constructor(
    private val users: UserRepository,
    private val api: VoltisApi,
    private val auth: AuthRepository,
    private val device: DeviceSettings,
    private val sync: SyncCenter,
    private val connectivity: Connectivity,
    downloads: DownloadRepository,
    store: SessionStore,
    events: CatalogEvents,
) : ViewModel() {
    val session = store.state
    val me = users.me

    /** Per device. Null until the stored one is read. */
    val theme = device.theme.stateIn(viewModelScope, SharingStarted.Eagerly, null)

    /** What signing out leaves on this device (P2 §11): the download rows and the unsent changes. */
    val leftBehind = combine(downloads.rows, sync.unsent) { rows, unsent -> AccountData(rows.size, unsent) }
        .stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), AccountData(0, 0))

    /** Why the account couldn't be loaded. */
    var error by mutableStateOf<UiText?>(null)
        private set

    var signingOut by mutableStateOf(false)
        private set

    /** No event changes the account: it is fetched again on a return after a while. */
    val refresher = Refresher(viewModelScope, events, matches = { it is CatalogChange.OnlineChanged }, refresh = ::load)

    /** The server's release, read from `GET /api/info`; null until known, and when the request fails. */
    var serverVersion by mutableStateOf<String?>(null)
        private set

    init {
        if (me.value == null) load() else loadVersion()
    }

    fun load() {
        error = null
        viewModelScope.launch { error = attempt { users.refresh() } }
        loadVersion()
    }

    private fun loadVersion() {
        viewModelScope.launch { serverVersion = attemptResult { api.info().version }.getOrNull()?.takeIf { it.isNotBlank() } }
    }

    fun setTheme(mode: ThemeMode) {
        viewModelScope.launch { device.setTheme(mode) }
    }

    /** The confirmation shows; first, online, the unsent changes had a few seconds to go. */
    var confirming by mutableStateOf(false)
        private set

    fun askSignOut() {
        if (signingOut) return
        signingOut = true
        viewModelScope.launch {
            try {
                if (connectivity.online.value && sync.unsent.first() > 0) withTimeoutOrNull(SIGN_OUT_DRAIN) { drain() }
            } finally {
                signingOut = false
            }
            confirming = true
        }
    }

    private suspend fun drain() {
        try {
            session.value.account?.let { sync.drain(it) }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            // No engine: what is unsent is counted as it is.
        }
    }

    fun dismissSignOut() {
        confirming = false
    }

    /** [delete]: the account's downloads and unsent changes go too ("Also delete them"). */
    fun signOut(delete: Boolean) {
        if (signingOut) return
        confirming = false
        signingOut = true
        // The session state changes and AppNav leaves the signed-in app.
        viewModelScope.launch {
            try {
                auth.logout(deleteData = delete)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                // Not signed out: StorageFullException (the alert says why) or another write failure. The session stays as
                // stored: a sign-out made only in memory would leave the token after a kill.
                Log.e("SettingsViewModel", "Sign-out failed", e)
                signingOut = false
            }
        }
    }

    private companion object {
        /** How long sign-out tries to send what is unsent first. */
        const val SIGN_OUT_DRAIN = 3_000L
    }
}
