package me.tijlvdb.voltis.ui.libraries

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.api.Library
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.data.user.UserRepository
import me.tijlvdb.voltis.data.sync.SyncCenter
import me.tijlvdb.voltis.ui.Refresher
import me.tijlvdb.voltis.ui.UiText

@HiltViewModel
class LibrariesViewModel @Inject constructor(
    private val users: UserRepository,
    downloads: DownloadRepository,
    sync: SyncCenter,
    session: SessionStore,
    events: CatalogEvents,
) : ViewModel() {
    /** Queued or running, for the Downloads row. */
    val activeDownloads = downloads.active

    /** Held items and stored notices: the Downloads row's badge. */
    val attention = session.state.value.account?.let(sync::attentionCount) ?: flowOf(0)

    /** Its preferences say which libraries are shown, moved to Others, or hidden. */
    val me = users.me

    var libraries by mutableStateOf<List<Library>?>(null)
        private set

    var error by mutableStateOf<UiText?>(null)
        private set

    val refresher = Refresher(viewModelScope, events, matches = { it == CatalogChange.PreferencesChanged || it is CatalogChange.OnlineChanged }) {
        load()
        // The visibility may have changed on another device.
        viewModelScope.launch { attempt { users.refresh() } }
    }

    init {
        load()
    }

    fun load() {
        error = null
        viewModelScope.launch { error = attempt { libraries = users.libraries() } }
    }
}
