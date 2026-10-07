package me.tijlvdb.voltis.ui.lists

import androidx.compose.foundation.focusable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.input.TextFieldState
import androidx.compose.foundation.text.input.clearText
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.InputMode
import androidx.compose.ui.platform.LocalInputModeManager
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.SavedStateHandleSaveableApi
import androidx.lifecycle.viewmodel.compose.saveable
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.ListVisibility
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.lists.Created
import me.tijlvdb.voltis.data.lists.ListView
import me.tijlvdb.voltis.data.lists.ListsRepository
import me.tijlvdb.voltis.data.lists.RefreshOutcome
import me.tijlvdb.voltis.data.lists.Write
import me.tijlvdb.voltis.data.lists.failureText
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.sync.EntryItem
import me.tijlvdb.voltis.ui.Effects
import me.tijlvdb.voltis.ui.OfflineBar
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.SnackbarsModel
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VCheckboxRow
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VSheet
import me.tijlvdb.voltis.ui.kit.VSpinner
import me.tijlvdb.voltis.ui.kit.VTextField

/** The key of [ListsSheetViewModel] under its host's page: the sheet and the page's effect host share the one instance. */
const val LISTS_SHEET = "lists-sheet"

/** The New list form of the current opening. */
enum class FormPhase { Closed, Editing, Creating }

/** What the sheet's writes ask of the page that hosts it, also after the sheet closed. */
sealed interface SheetEffect {
    data class Message(val text: UiText) : SheetEffect

    /** A selection's Add landed for [opening]; the host ends select mode only if that is its current one. */
    data class Added(val opening: Long, val text: UiText) : SheetEffect
}

/**
 * The add-to-list sheets' state. Each time the sheet is opened is an opening: its host passes the
 * opening's ID to [open] and [close]. The form, the choices and the errors belong to the current
 * opening; a write's outcome changes them only while the opening that started it is on screen.
 * Otherwise it is a message: a create's goes to the retained snackbars, other writes' to [effects].
 * The writes and their guards outlive the sheet.
 */
@HiltViewModel
@OptIn(SavedStateHandleSaveableApi::class)
class ListsSheetViewModel @Inject constructor(
    private val repository: ListsRepository,
    connectivity: Connectivity,
    private val snackbars: SnackbarsModel,
    private val session: SessionStore,
    private val saved: SavedStateHandle,
) : ViewModel() {
    val lists = repository.lists.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    val online = connectivity.online

    /** The account's store couldn't be opened. */
    val storeFailed = repository.storeFailed

    val effects = Effects<SheetEffect>()

    private var opening: Long?
        get() = saved[OPENING]
        set(value) {
            saved[OPENING] = value
        }

    /** The current opening's sheet is in a live composition, or across a rotation. */
    var presented by mutableStateOf(false)
        private set

    private var chosenState by mutableStateOf(saved.get<ArrayList<String>>(CHOSEN)?.toSet().orEmpty())
    private var formState by mutableStateOf(saved.get<String>(FORM)?.let(FormPhase::valueOf) ?: FormPhase.Closed)

    /** Selection mode's checked lists. */
    var chosen: Set<String>
        get() = chosenState
        private set(value) {
            chosenState = value
            saved[CHOSEN] = ArrayList(value)
        }

    var form: FormPhase
        get() = formState
        private set(value) {
            formState = value
            saved[FORM] = value.name
        }

    /** The New list name. */
    val name: TextFieldState = saved.saveable("name", saver = TextFieldState.Saver) { TextFieldState() }

    /** Create was pressed with a blank name. */
    var missing by mutableStateOf(false)
        private set

    var createError by mutableStateOf<UiText?>(null)
        private set

    /** The last write's failure: a snackbar would be under the sheet. */
    var error by mutableStateOf<UiText?>(null)
        private set

    /** The form closed: focus goes back to New list. */
    var refocusNewList by mutableStateOf(false)
        private set

    /** A list is being created, from any opening. Not while [adding]. */
    var creating by mutableStateOf(false)
        private set

    /** A selection's add is on its way. */
    var adding by mutableStateOf(false)
        private set

    /** Lists with a write on its way. */
    var pending by mutableStateOf(emptySet<String>())
        private set

    /** A refresh ran for the current opening: an empty index is the server's word. */
    var loaded by mutableStateOf(false)
        private set

    /** Why the index couldn't be loaded, or a write's reload failed; Retry only refreshes. */
    var loadError by mutableStateOf<UiText?>(null)
        private set

    var refreshing by mutableStateOf(false)
        private set

    /** The opening whose load this instance owes. */
    private var loadedFor: Long? = null
    private var refreshJob: Job? = null

    init {
        // The process died while a create ran: whether it landed is unknown, and the opening's load shows it.
        if (form == FormPhase.Creating) {
            error = UiText.Res(R.string.list_create_unknown, name.text.toString().trim())
            form = FormPhase.Closed
            name.clearText()
        }
    }

    fun holding(item: EntryItem?) = item?.let(repository::listsHolding) ?: flowOf(emptySet())

    /** The sheet of [opening] is on screen: a new opening starts fresh and loads once; the same one again keeps its state. */
    fun open(opening: Long) {
        if (this.opening != opening) {
            this.opening = opening
            chosen = emptySet()
            form = FormPhase.Closed
            name.clearText()
            error = null
            createError = null
            missing = false
            refocusNewList = false
        } else if (form == FormPhase.Creating && !creating) {
            // Its create finished while it was off screen; the outcome was a message.
            form = FormPhase.Closed
            name.clearText()
            refocusNewList = false
        }
        presented = true
        if (loadedFor != opening) {
            loadedFor = opening
            loaded = false
            load()
        }
    }

    /** The sheet left the screen for a reason other than rotation: dismissed, or its page left. */
    fun close(opening: Long) {
        if (this.opening == opening) presented = false
    }

    /** Retry: refreshes only; never sends a write again. */
    fun reload() = load()

    /** One loader; it ends only when the opening it last loaded for is the current one. */
    private fun load() {
        if (refreshJob?.isActive == true) return
        refreshJob = viewModelScope.launch {
            refreshing = true
            try {
                do {
                    val forOpening = loadedFor
                    loadError = null
                    val outcome = repository.refresh()
                    if (loadedFor == forOpening) settle(outcome)
                } while (loadedFor != forOpening)
            } finally {
                refreshing = false
            }
        }
    }

    fun choose(id: String, checked: Boolean) {
        chosen = if (checked) chosen + id else chosen - id
    }

    fun editNew() {
        form = FormPhase.Editing
        missing = false
        createError = null
    }

    fun cancelNew() {
        closeForm()
        missing = false
        createError = null
    }

    fun refocused() {
        refocusNewList = false
    }

    /** A content page's sheet: the change applies at once. */
    fun toggle(list: ListView, item: EntryItem, add: Boolean) {
        if (list.id in pending) return
        val op = opening
        pending = pending + list.id
        error = null
        viewModelScope.launch {
            try {
                if (add) {
                    when (val outcome = repository.addEntries(listOf(list.id), listOf(item))) {
                        is Write.Applied -> {
                            if (onScreen(op)) outcome.reload?.let(::settle)
                            if (outcome.value.missing?.contains(item.contentId) == true) {
                                fail(op, UiText.Format(R.string.lists_not_added, listOf(item.title, list.name)))
                            } else {
                                effects.post(SheetEffect.Message(UiText.Res(R.string.lists_added_to, list.name)))
                            }
                        }
                        is Write.Failed -> failed(op, outcome)
                    }
                } else {
                    when (val outcome = repository.removeEntry(list.id, item)) {
                        is Write.Applied -> {
                            if (onScreen(op)) outcome.reload?.let(::settle)
                            effects.post(SheetEffect.Message(UiText.Res(R.string.lists_removed_from, list.name)))
                        }
                        is Write.Failed -> failed(op, outcome)
                    }
                }
            } finally {
                pending = pending - list.id
            }
        }
    }

    /**
     * A private list from the typed name. A content page's sheet adds [items] to it, a selection's
     * checks it. Refused while any opening's create runs. It outlives this view model: when the page is
     * left mid-create, the outcome is still a message.
     */
    fun create(items: List<EntryItem>, selection: Boolean) {
        // One of Create and Add at a time: Add sends the chosen lists as they were pressed, so the new one wouldn't be in it.
        if (creating || adding) return
        missing = name.text.isBlank()
        if (missing) return
        val op = opening
        val typed = name.text.toString()
        creating = true
        createError = null
        form = FormPhase.Creating
        val account = session.state.value.account
        val write = snackbars.scope.async { repository.create(typed, null, ListVisibility.PRIVATE, if (selection) emptyList() else items) }
        viewModelScope.launch {
            try {
                val outcome = write.await()
                if (!onScreen(op)) {
                    // Not into the page's effects: a covered page may be popped before it shows them.
                    retained(account, { outcome }, selection)
                    return@launch
                }
                when (outcome) {
                    is Write.Applied -> {
                        outcome.reload?.let(::settle)
                        val list = outcome.value
                        closeForm()
                        when {
                            selection -> chosen = chosen + list.id
                            list.notAdded == null -> message(outcome.text(selection))
                            else -> {
                                // The list exists: its unchecked row is the way to add to it again.
                                message(UiText.Res(R.string.list_created, list.name))
                                error = outcome.text(selection)
                            }
                        }
                    }
                    // Not created: Create is safe to press again.
                    is Write.Failed -> {
                        form = FormPhase.Editing
                        createError = outcome.text
                    }
                    // Never Create again with this name: the refreshed lists show whether it landed.
                    is Write.Unknown -> {
                        settle(outcome.reload)
                        closeForm()
                        error = outcome.text
                    }
                }
            } catch (e: CancellationException) {
                // Cleared with its page while the create ran: nothing here is on screen, so the outcome is a message.
                retained(account, { write.await() }, selection)
                throw e
            } finally {
                // A cleared view model changes nothing.
                if (viewModelScope.isActive) creating = false
            }
        }
    }

    /** A selection's Add: every chosen list still shown, and every item, in one request; the message has the server's count. */
    fun add(items: List<EntryItem>) {
        val targets = targets()
        val op = opening
        if (adding || creating || targets.isEmpty() || op == null) return
        adding = true
        error = null
        viewModelScope.launch {
            try {
                when (val outcome = repository.addEntries(targets, items)) {
                    is Write.Applied -> {
                        if (onScreen(op)) outcome.reload?.let(::settle)
                        val missing = outcome.value.missing
                        val count = outcome.value.count
                        val counts = listOf(
                            UiText.Plural(R.plurals.lists_added_entry_count, count),
                            UiText.Plural(R.plurals.lists_added_list_count, targets.size),
                        )
                        val text = when {
                            // Content gone from the server is skipped too: only a reload that shows every item there proves it.
                            count == 0 && missing == null -> UiText.Res(R.string.lists_nothing_added)
                            missing.orEmpty().isNotEmpty() && count == 0 -> UiText.Plural(R.plurals.lists_not_added_count, missing!!.size)
                            missing.orEmpty().isNotEmpty() -> UiText.Format(
                                R.string.lists_added_partial,
                                listOf(UiText.Format(R.string.lists_added_entries, counts), UiText.Plural(R.plurals.lists_not_added_count, missing!!.size)),
                            )
                            count == 0 -> UiText.Plural(R.plurals.lists_already_in, targets.size)
                            else -> UiText.Format(R.string.lists_added_entries, counts)
                        }
                        effects.post(SheetEffect.Added(op, text))
                    }
                    is Write.Failed -> failed(op, outcome)
                }
            } finally {
                adding = false
            }
        }
    }

    /** The chosen lists that are shown: what Add sends. */
    fun targets(): List<String> {
        val shown = lists.value.orEmpty().mapTo(HashSet()) { it.id }
        return chosen.filter { it in shown }
    }

    private fun onScreen(op: Long?) = presented && op != null && opening == op

    private fun failed(op: Long?, outcome: Write.Failed) {
        if (onScreen(op)) outcome.reload?.let(::settle)
        fail(op, outcome.text)
    }

    /** In the sheet while [op] is on screen, else a message. */
    private fun fail(op: Long?, text: UiText) {
        if (onScreen(op)) error = text else message(text)
    }

    private fun message(text: UiText) = effects.post(SheetEffect.Message(text))

    /** A create's outcome as a retained snackbar, only while [account], which started it, is still signed in. */
    private fun retained(account: String?, outcome: suspend () -> Write<Created>, selection: Boolean) {
        snackbars.later { outcome().text(selection).takeIf { session.state.value.account == account } }
    }

    private fun closeForm() {
        form = FormPhase.Closed
        refocusNewList = true
        name.clearText()
    }

    private fun settle(outcome: RefreshOutcome) {
        if (outcome == RefreshOutcome.Ran) loaded = true
        loadError = outcome.failureText()
    }

    private companion object {
        const val OPENING = "opening"
        const val CHOSEN = "chosen"
        const val FORM = "form"
    }
}

/** What a create's outcome says, in a snackbar or the sheet's error. */
private fun Write<Created>.text(selection: Boolean): UiText = when (this) {
    is Write.Applied -> {
        val notAdded = value.notAdded
        when {
            selection -> UiText.Res(R.string.list_created, value.name)
            notAdded == null -> UiText.Res(R.string.lists_added_to, value.name)
            else -> UiText.Format(R.string.lists_created_not_added, listOf(value.name, notAdded))
        }
    }
    is Write.Failed -> text
    is Write.Unknown -> text
}

/**
 * Add to list (the web's two `ListsModal.vue`s). From a content page ([selection] false, one item)
 * each check shows and changes membership at once. From a selection the checks start empty and Add
 * sends them. Every control needs the server (P4 decision 14). The host calls
 * [ListsSheetViewModel.open] and [ListsSheetViewModel.close] for each opening and hosts its effects.
 */
@Composable
fun ListsSheet(items: List<EntryItem>, selection: Boolean, vm: ListsSheetViewModel, onDismiss: () -> Unit) {
    val lists by vm.lists.collectAsStateWithLifecycle()
    val online by vm.online.collectAsStateWithLifecycle()
    val storeFailed by vm.storeFailed.collectAsStateWithLifecycle()
    val single = items.singleOrNull().takeUnless { selection }
    val holding by remember(single) { vm.holding(single) }.collectAsStateWithLifecycle(emptySet())
    VSheet(
        stringResource(if (selection) R.string.lists_add_to_lists else R.string.lists_add_to_list),
        onDismiss,
        meta = if (selection) pluralStringResource(R.plurals.lists_items_selected, items.size, items.size) else null,
        action = if (vm.refreshing) ({ VSpinner(Modifier.size(20.dp)) }) else null,
    ) {
        if (storeFailed != null) {
            QueryError(UiText.Res(R.string.error_offline_data), Modifier.padding(bottom = 8.dp))
            return@VSheet
        }
        OfflineBar(Modifier.padding(bottom = 8.dp))
        // Offline, the bar above already says so: its Retry is this one.
        QueryError(vm.loadError.takeIf { online }, Modifier.padding(bottom = 8.dp), vm::reload)
        QueryError(vm.error, Modifier.padding(bottom = 8.dp))
        val current = lists.orEmpty()
        // Empty only on the server's word: not while this opening's first load runs or after it failed.
        if (current.isEmpty() && vm.loaded) {
            Text(stringResource(R.string.lists_empty), Modifier.padding(vertical = 12.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        for (list in current) {
            val label = stringResource(R.string.lists_sheet_row, list.name, visibilityLabel(list.visibility))
            if (single != null) {
                VCheckboxRow(label, list.id in holding, { vm.toggle(list, single, it) }, enabled = online && list.id !in vm.pending)
            } else {
                VCheckboxRow(label, list.id in vm.chosen, { vm.choose(list.id, it) }, enabled = online && !vm.adding)
            }
        }
        NewList(online, vm, items, selection)
        if (selection) {
            Row(Modifier.fillMaxWidth().padding(top = 8.dp), Arrangement.spacedBy(8.dp, Alignment.End)) {
                VButton(stringResource(R.string.cancel), onDismiss, style = VButtonStyle.Text)
                VButton(
                    stringResource(R.string.lists_add),
                    { vm.add(items) },
                    enabled = online && !vm.adding && !vm.creating && vm.targets().isNotEmpty(),
                )
            }
        }
    }
}

/**
 * Focus for the button while it is disabled ([enabled] here), from the keyboard only, shown by a ring
 * around its 40 dp pill. Always in the chain, so the button keeps one focus order offline and online.
 */
@Composable
private fun Modifier.keyboardFocusable(enabled: Boolean): Modifier {
    val input = LocalInputModeManager.current
    val source = remember { MutableInteractionSource() }
    val focused by source.collectIsFocusedAsState()
    val ring = MaterialTheme.colorScheme.primary
    return drawWithContent {
        drawContent()
        if (focused) {
            // The touch target is taller than the pill it centres.
            val height = minOf(size.height, 40.dp.toPx())
            val stroke = 2.dp.toPx()
            drawRoundRect(
                ring,
                Offset(stroke / 2, (size.height - height + stroke) / 2),
                Size(size.width - stroke, height - stroke),
                CornerRadius(height / 2),
                Stroke(stroke),
            )
        }
    }
        .focusProperties { canFocus = enabled && input.inputMode == InputMode.Keyboard }
        .focusable(interactionSource = source)
}

/** "New list", which turns into a name field with Create and Cancel (the web's `NewListForm.vue`). */
@Composable
private fun NewList(online: Boolean, vm: ListsSheetViewModel, items: List<EntryItem>, selection: Boolean) {
    if (vm.form == FormPhase.Closed) {
        val button = remember { FocusRequester() }
        // Only when the form closed, not when the sheet opens.
        LaunchedEffect(vm.refocusNewList) { if (vm.refocusNewList && button.requestFocus()) vm.refocused() }
        // Disabled, it held keyboard focus; enabled, focus moves to its own target rather than being dropped.
        var held by remember { mutableStateOf(false) }
        LaunchedEffect(online) {
            if (online && held) {
                held = false
                button.requestFocus()
            }
        }
        VButton(
            stringResource(R.string.lists_new),
            vm::editNew,
            // Disabled offline, it still takes the focus the form leaves: nothing else in the sheet could but its handle.
            Modifier.focusRequester(button).onFocusChanged { if (!online) held = it.isFocused }.keyboardFocusable(!online),
            style = VButtonStyle.Text,
            enabled = online,
            icon = VIcons.Plus,
        )
        return
    }
    val field = remember { FocusRequester() }
    // Also back from Creating after a failed create: the disabled field lost its focus.
    LaunchedEffect(vm.form) { if (vm.form == FormPhase.Editing) field.requestFocus() }
    val sending = vm.form == FormPhase.Creating
    val submit = { vm.create(items, selection) }
    VTextField(
        vm.name,
        stringResource(R.string.lists_new_name),
        Modifier.fillMaxWidth().padding(top = 8.dp).focusRequester(field),
        enabled = !sending,
        maxLength = 100,
        error = if (vm.missing && vm.name.text.isBlank()) stringResource(R.string.list_name_required) else null,
        // The keyboard can cover Create.
        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
        onKeyboardAction = { if (!vm.creating && !vm.adding && online) submit() },
    )
    QueryError(vm.createError)
    Row(Modifier.fillMaxWidth(), Arrangement.spacedBy(8.dp, Alignment.End)) {
        VButton(stringResource(R.string.cancel), vm::cancelNew, style = VButtonStyle.Text, enabled = !sending)
        // Disabled while any opening's create runs.
        VButton(stringResource(R.string.list_create_confirm), submit, style = VButtonStyle.Tonal, enabled = online && !vm.creating && !vm.adding)
    }
}
