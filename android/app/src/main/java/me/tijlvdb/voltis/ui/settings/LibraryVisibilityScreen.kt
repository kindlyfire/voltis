package me.tijlvdb.voltis.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Library
import me.tijlvdb.voltis.data.api.LibraryVisibility
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.user.UserRepository
import me.tijlvdb.voltis.domain.catalog.VisibilityEdits
import me.tijlvdb.voltis.ui.LoadStatus
import me.tijlvdb.voltis.ui.PushedScreen
import me.tijlvdb.voltis.ui.RefreshOnResume
import me.tijlvdb.voltis.ui.Refresher
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VSegmented

@HiltViewModel
class LibraryVisibilityViewModel @Inject constructor(
    private val saved: SavedStateHandle,
    private val users: UserRepository,
    events: CatalogEvents,
) : ViewModel() {
    /** Its preferences hold the saved visibilities. */
    val me = users.me

    var libraries by mutableStateOf<List<Library>?>(null)
        private set

    var error by mutableStateOf<UiText?>(null)
        private set

    /** The choices not saved yet, by library. In saved state: they come back after process death. */
    var edits by mutableStateOf<Map<String, String>>(saved.get<HashMap<String, String>>(EDITS).orEmpty())
        private set

    var saving by mutableStateOf(false)
        private set

    var saveError by mutableStateOf<UiText?>(null)
        private set

    private val _saved = Channel<Unit>(Channel.BUFFERED)
    val savedEvents = _saved.receiveAsFlow()

    /** The visibility may have been changed on the web: fetched again on a return after a while, under the unsaved choices. */
    val refresher = Refresher(viewModelScope, events, matches = { it is CatalogChange.OnlineChanged }, refresh = ::load)

    init {
        load()
    }

    fun load() {
        error = null
        viewModelScope.launch {
            error = attempt {
                users.refresh()
                libraries = users.libraries()
            }
        }
    }

    fun visibility(libraryId: String) = edits[libraryId] ?: stored(libraryId)

    private fun stored(libraryId: String) = me.value?.prefs?.libraryVisibility(libraryId) ?: LibraryVisibility.SHOW

    /** What the save on its way submitted. */
    private var sent: Map<String, String>? = null

    fun set(libraryId: String, visibility: String) {
        put(VisibilityEdits.choose(edits, libraryId, visibility, stored(libraryId), sent))
    }

    private fun put(edits: Map<String, String>) {
        this.edits = edits
        saved[EDITS] = HashMap(edits)
    }

    fun save() {
        if (saving || edits.isEmpty()) return
        saving = true
        saveError = null
        val sent = edits.also { sent = it }
        viewModelScope.launch {
            saveError = attempt { users.patchPreferences(VisibilityEdits.patch(sent)) }
            saving = false
            this@LibraryVisibilityViewModel.sent = null
            if (saveError != null) return@launch
            put(VisibilityEdits.unsaved(edits, sent))
            _saved.send(Unit)
        }
    }

    private companion object {
        const val EDITS = "edits"
    }
}

/** Show, Overflow (under "Others") or Hide per library, saved together: the card of `InterfacePage.vue`. */
@Composable
fun LibraryVisibilityScreen(onBack: () -> Unit, vm: LibraryVisibilityViewModel = hiltViewModel()) {
    RefreshOnResume(vm.refresher)
    val snackbars = LocalSnackbars.current
    val saved = stringResource(R.string.visibility_saved)
    LaunchedEffect(vm) { vm.savedEvents.collect { snackbars.show(saved) } }
    // Read so a fetched user recomposes the controls.
    val me by vm.me.collectAsStateWithLifecycle()
    val options = listOf(
        LibraryVisibility.SHOW to stringResource(R.string.visibility_show),
        LibraryVisibility.OVERFLOW to stringResource(R.string.visibility_overflow),
        LibraryVisibility.HIDE to stringResource(R.string.visibility_hide),
    )
    PushedScreen(stringResource(R.string.visibility_title), onBack) {
        LoadStatus(vm.libraries == null, vm.error, vm::load)
        val libraries = vm.libraries ?: return@PushedScreen
        if (me == null) return@PushedScreen
        if (libraries.isEmpty()) Text(stringResource(R.string.home_no_libraries), color = MaterialTheme.colorScheme.onSurfaceVariant)
        for (library in libraries) {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                // The control below is named after the library.
                Row(Modifier.clearAndSetSemantics {}, Arrangement.spacedBy(12.dp), Alignment.CenterVertically) {
                    Icon(VIcons.Bookshelf, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    Text(library.name, style = MaterialTheme.typography.bodyLarge)
                }
                VSegmented(stringResource(R.string.visibility_group, library.name), options, vm.visibility(library.id), { vm.set(library.id, it) })
            }
        }
        QueryError(vm.saveError)
        VButton(stringResource(R.string.save), vm::save, Modifier.align(Alignment.End), enabled = !vm.saving && vm.edits.isNotEmpty())
    }
}
