package me.tijlvdb.voltis.ui.home

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.Job
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContinueEntry
import me.tijlvdb.voltis.data.api.Library
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.api.attemptResult
import me.tijlvdb.voltis.data.api.toUiText
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.content.ContentListParams
import me.tijlvdb.voltis.data.content.ContentRepository
import me.tijlvdb.voltis.data.content.ContentSort
import me.tijlvdb.voltis.data.content.GridFilters
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.data.settings.DeviceSettings
import me.tijlvdb.voltis.data.user.UserRepository
import me.tijlvdb.voltis.domain.catalog.GridOptions
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.net.settled
import me.tijlvdb.voltis.ui.Refresher
import me.tijlvdb.voltis.ui.UiText

/** One Home row: loading until it has items or an error. A failed refresh keeps the items it had. */
data class HomeRow<T>(val items: List<T>? = null, val error: UiText? = null) {
    val loading get() = items == null && error == null
}

@HiltViewModel
class HomeViewModel @Inject constructor(
    private val content: ContentRepository,
    private val users: UserRepository,
    settings: DeviceSettings,
    events: CatalogEvents,
    downloads: DownloadRepository,
    private val connectivity: Connectivity,
) : ViewModel() {
    val me = users.me

    /** The cards' download marks. */
    val badges = downloads.badges

    /** The cards follow the display options of the library grids. */
    val options = settings.gridOptions.stateIn(viewModelScope, SharingStarted.Eagerly, GridOptions())

    /** Null until loaded: an empty list is the "No libraries yet" state. */
    var libraries by mutableStateOf<List<Library>?>(null)
        private set

    var reading by mutableStateOf(HomeRow<ContinueEntry>())
        private set

    var updated by mutableStateOf(HomeRow<Content>())
        private set

    var added by mutableStateOf(HomeRow<Content>())
        private set

    var refreshing by mutableStateOf(false)
        private set

    /** Every catalog change can move a Home row. */
    val refresher = Refresher(viewModelScope, events, matches = { true }, refresh = ::load)

    private var job: Job? = null

    init {
        load()
    }

    /** Pull-to-refresh and Retry: the indicator shows until every row has answered. */
    fun refresh() {
        refreshing = true
        load()
    }

    private fun load() {
        job?.cancel()
        job = viewModelScope.launch {
            // Offline the screen shows the offline state, and reloads once the server is back.
            connectivity.settled()
            if (!connectivity.online.value) {
                refreshing = false
                return@launch
            }
            coroutineScope {
                launch { attempt { libraries = users.libraries() } }
                launch { reading = fetch(reading) { content.continueReading(ROW_SIZE) } }
                launch { updated = fetch(updated) { content.list(row(ContentSort.RECENTLY_UPDATED)).data } }
                launch { added = fetch(added) { content.list(row(ContentSort.CREATED_AT).copy(parentId = ContentListParams.TOP_LEVEL)).data } }
            }
            refreshing = false
        }
    }

    private fun row(sort: String) = ContentListParams(sort = sort, sortOrder = GridFilters.DESC, limit = ROW_SIZE, count = false)

    private suspend fun <T> fetch(row: HomeRow<T>, get: suspend () -> List<T>): HomeRow<T> =
        attemptResult { HomeRow(get()) }.getOrElse { HomeRow(row.items, it.toUiText()) }

    private companion object {
        const val ROW_SIZE = 10
    }
}
