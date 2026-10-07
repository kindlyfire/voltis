package me.tijlvdb.voltis.ui.lists

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.lists.ListsRepository
import me.tijlvdb.voltis.data.lists.RefreshOutcome
import me.tijlvdb.voltis.data.lists.failureText
import me.tijlvdb.voltis.data.lists.text
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.ui.Effects
import me.tijlvdb.voltis.ui.Refresher
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.kit.SnackbarsModel

/** The cached lists at once; a refresh when the screen opens and on pull. Create and edit online. */
@HiltViewModel
class ListsViewModel @Inject constructor(
    private val repository: ListsRepository,
    connectivity: Connectivity,
    events: CatalogEvents,
    snackbars: SnackbarsModel,
    session: SessionStore,
) : ViewModel() {
    /** The account's store couldn't be opened. */
    val storeFailed = repository.storeFailed

    /** Null until the cache has been read, and while no store is open. */
    val lists = repository.lists.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    /** A refresh is running. */
    var busy by mutableStateOf(false)
        private set

    /** A refresh the user pulled for is running: the indicator shows. */
    var refreshing by mutableStateOf(false)
        private set

    /** The last refresh failed; the cached lists stay. */
    var error by mutableStateOf<UiText?>(null)
        private set

    private var job: Job? = null

    /** Edits need the server (P4 decision 14). */
    val online = connectivity.online

    val effects = Effects<ListEffect>()

    val editor = ListEditor(repository, viewModelScope, snackbars, { session.state.value.account }, effects, { error = it.failureText() })

    /** Lists change only through this app's own writes, which refresh themselves: only the server coming or going. */
    val refresher = Refresher(viewModelScope, events, matches = { it is CatalogChange.OnlineChanged }) { refresh(pulled = false) }

    init {
        refresh(pulled = false)
    }

    fun refresh(pulled: Boolean = true) {
        if (pulled) refreshing = true
        if (job?.isActive == true) return
        job = viewModelScope.launch {
            busy = true
            try {
                error = (repository.refresh() as? RefreshOutcome.Failed)?.text()
            } finally {
                busy = false
                refreshing = false
            }
        }
    }
}
