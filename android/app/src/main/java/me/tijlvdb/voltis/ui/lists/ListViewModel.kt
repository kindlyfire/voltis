package me.tijlvdb.voltis.ui.lists

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.assisted.Assisted
import dagger.assisted.AssistedFactory
import dagger.assisted.AssistedInject
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.attemptResult
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.lists.EntryView
import me.tijlvdb.voltis.data.lists.ListsRepository
import me.tijlvdb.voltis.data.lists.RefreshOutcome
import me.tijlvdb.voltis.data.lists.Write
import me.tijlvdb.voltis.data.lists.failureText
import me.tijlvdb.voltis.data.lists.text
import me.tijlvdb.voltis.data.user.UserRepository
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.ui.Effects
import me.tijlvdb.voltis.ui.Refresher
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.kit.SnackbarKey
import me.tijlvdb.voltis.ui.kit.SnackbarsModel

/**
 * A list's page from the cache, refreshed when it opens, on pull and by its [refresher]. Its edits
 * are direct and online only (P4 decision 14); one entry write at a time.
 */
@HiltViewModel(assistedFactory = ListViewModel.Factory::class)
class ListViewModel @AssistedInject constructor(
    @Assisted private val id: String,
    private val repository: ListsRepository,
    private val users: UserRepository,
    private val connectivity: Connectivity,
    events: CatalogEvents,
    private val snackbars: SnackbarsModel,
    private val session: SessionStore,
) : ViewModel() {
    @AssistedFactory
    interface Factory {
        fun create(id: String): ListViewModel
    }

    /** Null until the cache has been read. */
    val cached = repository.list(id).stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    /** The account's store couldn't be opened. */
    val storeFailed = repository.storeFailed

    var busy by mutableStateOf(false)
        private set

    /** A refresh the user pulled for is running: the indicator shows. */
    var refreshing by mutableStateOf(false)
        private set

    /** The last refresh failed; what is cached stays. */
    var error by mutableStateOf<UiText?>(null)
        private set

    /** The server said "List not found" to the last refresh. */
    var gone by mutableStateOf(false)
        private set

    /** The last refresh found the server unreachable. */
    var offline by mutableStateOf(false)
        private set

    /** Library names for the entries' chips; null offline or until loaded, and then there are no chips. */
    var libraries by mutableStateOf<Map<String, String>?>(null)
        private set

    private var job: Job? = null
    private var librariesJob: Job? = null

    /** Edits need the server. */
    val online = connectivity.online

    val effects = Effects<ListEffect>()

    /**
     * Bumped by anything the user does on the page, and by the page pausing or losing window focus: a
     * write's focus is moved only while it is what it was at the request (P4 §11). Kept across rotation.
     */
    var attention by mutableIntStateOf(0)
        private set

    fun attend() {
        attention++
    }

    val editor = ListEditor(repository, viewModelScope, snackbars, { session.state.value.account }, effects, ::reloaded, ::attend)

    /** The entry whose notes are being edited. */
    var notesFor by mutableStateOf<EntryView?>(null)
        private set

    var notesError by mutableStateOf<UiText?>(null)
        private set

    /** The notes dialog's host is composed, or rotating. */
    private var notesPresented = false

    /** An entry write (move, remove, notes) is on its way. */
    var writing by mutableStateOf(false)
        private set

    /** A move's answer was lost and its reload didn't run: the order may be stale until a refresh runs. */
    var movesBlocked by mutableStateOf(false)
        private set

    /** Lists change elsewhere without an event: only the 30-second rule. */
    val refresher = Refresher(viewModelScope, events, matches = { it is CatalogChange.OnlineChanged }, refresh = { refresh(pulled = false) })

    init {
        refresh(pulled = false)
    }

    /** [pulled]: the user's pull, which shows the indicator. */
    fun refresh(pulled: Boolean = true) {
        if (pulled) attend()
        loadLibraries()
        if (pulled) refreshing = true
        if (job?.isActive == true) return
        job = viewModelScope.launch {
            busy = true
            try {
                val outcome = repository.refreshList(id)
                gone = outcome == RefreshOutcome.Gone
                offline = outcome == RefreshOutcome.Offline
                error = (outcome as? RefreshOutcome.Failed)?.text()
                if (outcome == RefreshOutcome.Ran) {
                    movesBlocked = false
                    loadLibraries()
                }
            } finally {
                busy = false
                refreshing = false
            }
        }
    }

    fun presentNotes() {
        notesPresented = true
    }

    /** The page left other than by rotation: a running save no longer owns the notes dialog, which closes. */
    fun leaveNotes() {
        notesPresented = false
        if (writing) {
            notesFor = null
            notesError = null
        }
    }

    /** Null closes the dialog, also while its save runs: the outcome then is a message. None opens during a write. */
    fun editNotes(entry: EntryView?) {
        attend()
        if (writing && entry != null) return
        notesFor = entry
        notesError = null
    }

    fun saveNotes(notes: String) {
        attend()
        val entry = notesFor ?: return
        if (writing) return
        writing = true
        val started = session.state.value.account
        // Retained, so that a page popped meanwhile still gets its message, once and only for the account that saved.
        val run = snackbars.scope.async { repository.setNotes(id, entry.item(), notes) }
        val saved = UiText.Res(R.string.entry_notes_saved)
        viewModelScope.launch {
            try {
                val outcome = run.await()
                // Only the dialog that started it is changed, and only on screen.
                val here = notesPresented && notesFor === entry
                when (outcome) {
                    is Write.Applied -> {
                        outcome.reload?.let(::reloaded)
                        if (here) notesFor = null
                        say(started, saved)
                    }
                    is Write.Failed -> {
                        outcome.reload?.let(::reloaded)
                        if (here) notesError = outcome.text else say(started, outcome.text)
                    }
                }
            } catch (e: CancellationException) {
                snackbars.later {
                    when (val outcome = run.await()) {
                        is Write.Applied -> saved
                        is Write.Failed -> outcome.text
                    }.takeIf { session.state.value.account == started }
                }
                throw e
            } finally {
                writing = false
            }
        }
    }

    /** A message: the page's, while it is composed; else retained, for a covered page may be popped before it shows its effects. */
    private fun say(started: String?, text: UiText) {
        if (notesPresented) effects.post(ListEffect.Message(text)) else snackbars.later { text.takeIf { session.state.value.account == started } }
    }

    /** [delta] -1 is up. At an end it only says so. [exploring]: TalkBack is on, so focus may follow the row. */
    fun move(entry: EntryView, delta: Int, exploring: Boolean) {
        attend()
        val focus = FocusIntent(attention).takeIf { exploring }
        val entries = cached.value?.detail?.entries ?: return
        val at = entries.indexOfFirst { it.entryId == entry.entryId }
        if (at < 0) return
        val boundary = UiText.Res(if (delta < 0) R.string.entry_already_first else R.string.entry_already_last)
        if (at + delta !in entries.indices) {
            effects.post(ListEffect.Announce(boundary))
            return
        }
        if (movesBlocked) return
        entryWrite {
            when (val outcome = repository.move(id, entry.item(), delta)) {
                is Write.Applied -> {
                    outcome.reload?.let(::reloaded)
                    val moved = outcome.value
                    val place = moved?.place
                    when {
                        moved == null -> effects.post(ListEffect.Announce(boundary))
                        // Only the reload's verified place is said.
                        outcome.reload == RefreshOutcome.Ran && place != null -> effects.post(ListEffect.Moved(moved.entryId, place, focus, moved.revision))
                    }
                }
                is Write.Failed -> {
                    outcome.reload?.let(::reloaded)
                    effects.post(ListEffect.Message(outcome.text))
                }
                is Write.Unknown -> {
                    reloaded(outcome.reload)
                    if (outcome.reload != RefreshOutcome.Ran) movesBlocked = true
                    effects.post(ListEffect.Message(outcome.text))
                }
            }
        }
    }

    /** At once, without a confirmation (P4 decision 26). [exploring] as for [move]. */
    fun remove(entry: EntryView, exploring: Boolean) {
        attend()
        val focus = FocusIntent(attention).takeIf { exploring }
        val order = cached.value?.detail?.entries?.map { it.entryId } ?: return
        entryWrite {
            when (val outcome = repository.removeEntry(id, entry.item())) {
                is Write.Applied -> {
                    outcome.reload?.let(::reloaded)
                    val key = SnackbarKey()
                    effects.post(ListEffect.Message(UiText.Res(R.string.entry_removed, entry.title), key))
                    if (focus != null && outcome.reload == RefreshOutcome.Ran) {
                        effects.post(ListEffect.Removed(setOfNotNull(entry.entryId, outcome.value.entryId), order, key, focus, outcome.value.revision))
                    }
                }
                is Write.Failed -> {
                    outcome.reload?.let(::reloaded)
                    effects.post(ListEffect.Message(outcome.text))
                }
            }
        }
    }

    private fun entryWrite(block: suspend () -> Unit) {
        if (writing) return
        writing = true
        viewModelScope.launch {
            try {
                block()
            } finally {
                writing = false
            }
        }
    }

    /** A write's reload: one that didn't run leaves the error with its Retry, which only refreshes. */
    private fun reloaded(outcome: RefreshOutcome) {
        if (outcome == RefreshOutcome.Gone) gone = true
        if (outcome == RefreshOutcome.Ran) movesBlocked = false
        error = outcome.failureText()
        offline = outcome == RefreshOutcome.Offline
    }

    /** Beside the refresh, online only. */
    private fun loadLibraries() {
        if (libraries != null || librariesJob?.isActive == true || !connectivity.online.value) return
        librariesJob = viewModelScope.launch {
            attemptResult { users.libraries() }.onSuccess { all -> libraries = all.associate { it.id to it.name } }
        }
    }
}
