package me.tijlvdb.voltis.ui.discover

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.assisted.Assisted
import dagger.assisted.AssistedFactory
import dagger.assisted.AssistedInject
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.api.FacetEntry
import me.tijlvdb.voltis.data.api.Library
import me.tijlvdb.voltis.data.api.attemptResult
import me.tijlvdb.voltis.data.api.serverMessage
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.facets.FacetRepository
import me.tijlvdb.voltis.data.user.UserRepository
import me.tijlvdb.voltis.ui.Refresher
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.net.settled
import me.tijlvdb.voltis.ui.UiText
import retrofit2.HttpException

/** A Discover value: its name, count and roles in the chosen library. Its grid has a view model of its own. */
@HiltViewModel(assistedFactory = FacetViewModel.Factory::class)
class FacetViewModel @AssistedInject constructor(
    @Assisted("kind") private val kind: String,
    @Assisted("key") private val key: String,
    private val facets: FacetRepository,
    private val users: UserRepository,
    private val connectivity: Connectivity,
    events: CatalogEvents,
) : ViewModel() {
    @AssistedFactory
    interface Factory {
        fun create(@Assisted("kind") kind: String, @Assisted("key") key: String): FacetViewModel
    }

    /** The last answer; kept while another library's loads. */
    var entry by mutableStateOf<FacetEntry?>(null)
        private set

    /** The server has no such value. */
    var notFound by mutableStateOf(false)
        private set

    var error by mutableStateOf<UiText?>(null)
        private set

    /** The library select's options; null until they have loaded. */
    var libraries by mutableStateOf<List<Library>?>(null)
        private set

    /** The options failed to load; the entry and the grid show regardless. */
    var librariesError by mutableStateOf<UiText?>(null)
        private set

    private var librariesJob: Job? = null

    /** The library [entry] is for; null for every library. */
    private var libraryId: String? = null

    private var loaded = false

    private var job: Job? = null

    val refresher = Refresher(viewModelScope, events, matches = { it is CatalogChange.OnlineChanged }, refresh = ::reload)

    /** Loads the entry for [libraryId], unless it is the one already loaded. */
    fun show(libraryId: String?) {
        if (loaded && libraryId == this.libraryId) return
        this.libraryId = libraryId
        loaded = true
        reload()
    }

    /** Apart from the entry, so neither waits for nor hides the other. */
    fun loadLibraries() {
        if (libraries != null || librariesJob?.isActive == true) return
        librariesError = null
        librariesJob = viewModelScope.launch {
            connectivity.settled()
            attemptResult { users.libraries() }.onSuccess { libraries = it }.onFailure { librariesError = it.toUiText() }
        }
    }

    fun reload() {
        loadLibraries()
        job?.cancel()
        error = null
        val library = libraryId
        job = viewModelScope.launch {
            connectivity.settled()
            attemptResult { facets.entry(kind, key, library) }
                .onSuccess {
                    entry = it
                    notFound = false
                }
                .onFailure {
                    // Voltis' own 404, not a proxy's.
                    val response = (it as? HttpException)?.response()
                    if (response?.code() == 404 && response.serverMessage() != null) notFound = true else error = it.toUiText()
                }
        }
    }
}
