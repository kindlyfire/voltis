package me.tijlvdb.voltis.ui.lists

import androidx.activity.compose.LocalActivity
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.input.rememberTextFieldState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.ListVisibility
import me.tijlvdb.voltis.data.lists.EntryView
import me.tijlvdb.voltis.ui.OfflineBar
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VDialog
import me.tijlvdb.voltis.ui.kit.VSelect
import me.tijlvdb.voltis.ui.kit.VTextField

/** The open dialog of [editor] (the web's `ListModal.vue`). Offline its writes are disabled, and it says why; Cancel stays, also while a write runs. */
@Composable
fun ListDialogs(editor: ListEditor, online: Boolean) {
    Presented(editor::present, editor::leave)
    when (val dialog = editor.dialog) {
        ListDialog.Create -> ListForm(null, editor, online)
        is ListDialog.Edit -> ListForm(dialog, editor, online)
        is ListDialog.Delete -> VDialog(
            stringResource(R.string.list_delete_title),
            stringResource(R.string.list_delete),
            { editor.delete(dialog.list) },
            { editor.show(null) },
            stringResource(R.string.list_delete_text, dialog.list.name),
            confirmEnabled = online,
            danger = true,
            busy = editor.busy,
            error = editor.error,
            dismissable = true,
        ) { OfflineBar() }
        null -> Unit
    }
}

/** [present] while a dialog host is composed, [leave] when it goes; a rotation isn't leaving (as the lists sheet's). */
@Composable
fun Presented(present: () -> Unit, leave: () -> Unit) {
    val activity = LocalActivity.current
    DisposableEffect(Unit) {
        present()
        onDispose { if (activity?.isChangingConfigurations != true) leave() }
    }
}

/** Create, or with [edit] Edit with Delete. */
@Composable
private fun ListForm(edit: ListDialog.Edit?, editor: ListEditor, online: Boolean) {
    val list = edit?.list
    val name = rememberTextFieldState(list?.name.orEmpty())
    val description = rememberTextFieldState(list?.description.orEmpty())
    var visibility by rememberSaveable { mutableStateOf(list?.visibility ?: ListVisibility.PRIVATE) }
    var nameMissing by rememberSaveable { mutableStateOf(false) }
    VDialog(
        stringResource(if (list == null) R.string.lists_create else R.string.list_edit),
        stringResource(if (list == null) R.string.list_create_confirm else R.string.save),
        onConfirm = {
            nameMissing = name.text.isBlank()
            if (!nameMissing) editor.save(name.text.toString(), description.text.toString(), visibility)
        },
        onDismiss = { editor.show(null) },
        confirmEnabled = online,
        busy = editor.busy,
        error = editor.error,
        dismissable = true,
    ) {
        OfflineBar()
        VTextField(
            name,
            stringResource(R.string.list_name),
            Modifier.fillMaxWidth(),
            enabled = !editor.busy,
            maxLength = 100,
            error = if (nameMissing && name.text.isBlank()) stringResource(R.string.list_name_required) else null,
        )
        VTextField(description, stringResource(R.string.list_description), Modifier.fillMaxWidth(), enabled = !editor.busy, lines = 3, maxLength = 5000)
        VSelect(
            stringResource(R.string.list_visibility),
            listOf(ListVisibility.PUBLIC, ListVisibility.PRIVATE, ListVisibility.UNLISTED).map { it to visibilityLabel(it) },
            visibility,
            { visibility = it },
            // A list always has a visibility: no clear button.
            enabled = !editor.busy,
        )
        if (list != null) {
            // AlertDialog has no slot at the buttons' start: Delete ends the form.
            VButton(
                stringResource(R.string.list_delete),
                { editor.show(ListDialog.Delete(list)) },
                style = VButtonStyle.Text,
                enabled = online && !editor.busy,
                danger = true,
            )
        }
    }
}

/** The web's `EntryModal.vue`: the entry's title, then its notes. Blank saves no notes. */
@Composable
fun NotesDialog(entry: EntryView, online: Boolean, busy: Boolean, error: UiText?, onSave: (String) -> Unit, onDismiss: () -> Unit) {
    val notes = rememberTextFieldState(entry.notes.orEmpty())
    VDialog(
        stringResource(R.string.entry_edit_notes),
        stringResource(R.string.save),
        { onSave(notes.text.toString()) },
        onDismiss,
        entry.title,
        confirmEnabled = online,
        busy = busy,
        error = error,
        dismissable = true,
    ) {
        OfflineBar()
        VTextField(notes, stringResource(R.string.list_notes), Modifier.fillMaxWidth(), enabled = !busy, lines = 4)
    }
}
