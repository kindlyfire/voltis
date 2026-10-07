package me.tijlvdb.voltis.ui.lists

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.async
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.lists.ListView
import me.tijlvdb.voltis.data.lists.ListsRepository
import me.tijlvdb.voltis.data.lists.Place
import me.tijlvdb.voltis.data.lists.RefreshOutcome
import me.tijlvdb.voltis.data.lists.Write
import me.tijlvdb.voltis.ui.Effects
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.kit.SnackbarKey
import me.tijlvdb.voltis.ui.kit.SnackbarsModel

sealed interface ListDialog {
    data object Create : ListDialog

    data class Edit(val list: ListView) : ListDialog

    data class Delete(val list: ListView) : ListDialog
}

/** Focus may follow a write only while the screen's attention is still [attention] (P4 §11): TalkBack on, the user nowhere else. */
data class FocusIntent(val attention: Int)

/** What a lists screen asks of its composable, kept until done. */
sealed interface ListEffect {
    /** A snackbar; [key] marks it for a [Removed] that waits for it. */
    data class Message(val text: UiText, val key: SnackbarKey? = null) : ListEffect

    /** Said to TalkBack. */
    data class Announce(val text: UiText) : ListEffect

    /**
     * [place]: the entry's verified place after the move's reload, which is cache [revision]. The
     * handover waits while an older revision is displayed, and goes by the live rows from a newer one.
     */
    data class Moved(val entryId: String, val place: Place, val focus: FocusIntent?, val revision: Long) : ListEffect

    /**
     * Focus to the next row once the removal's message [after] has left the screen: a snackbar
     * appearing moves TalkBack to the top (the spike). [gone]: the requested row's and the deleted
     * entry's IDs. [order]: the displayed IDs at the request. [revision]: of the reload's cache.
     */
    data class Removed(val gone: Set<String>, val order: List<String>, val after: SnackbarKey, val focus: FocusIntent, val revision: Long) : ListEffect

    data class Deleted(val listId: String) : ListEffect
}

/**
 * The create, edit and delete dialogs of a screen's view model: one write at a time; a failure stays
 * in the dialog. A dialog can be dismissed while its write runs, and none opens until it ends: the
 * outcome then is a message, as when the dialogs' host left the screen meanwhile. A write that landed but whose reload failed goes to [reloaded], for the
 * screen's error with its refresh-only Retry. [attend] runs first in each user action.
 * The write runs in the retained snackbars' scope: when the screen is cleared before it ends (the
 * page popped), its outcome is still a message, and only while the account that started it is signed in.
 */
class ListEditor(
    private val repository: ListsRepository,
    private val scope: CoroutineScope,
    private val snackbars: SnackbarsModel,
    /** The signed-in account: an outcome shown after the screen is gone is only for the one that started it. */
    private val account: () -> String?,
    private val effects: Effects<ListEffect>,
    private val reloaded: (RefreshOutcome) -> Unit,
    private val attend: () -> Unit = {},
) {
    var dialog by mutableStateOf<ListDialog?>(null)
        private set

    var busy by mutableStateOf(false)
        private set

    var error by mutableStateOf<UiText?>(null)
        private set

    /** The list being deleted: its page shows nothing rather than "no longer exists" until it closes. */
    var deleting by mutableStateOf<String?>(null)
        private set

    /** The dialogs' host is composed, or rotating (as the lists sheet's). */
    private var presented = false

    fun present() {
        presented = true
    }

    /** The host left other than by rotation: a running write no longer owns its dialog, which closes. */
    fun leave() {
        presented = false
        if (busy) {
            dialog = null
            error = null
        }
    }

    fun show(dialog: ListDialog?) {
        attend()
        if (busy && dialog != null) return
        this.dialog = dialog
        error = null
    }

    fun save(name: String, description: String, visibility: String) {
        attend()
        when (val open = dialog) {
            ListDialog.Create -> launchWrite({ repository.create(name, description, visibility) }, { UiText.Res(R.string.list_created, it.name) })
            is ListDialog.Edit -> launchWrite({ repository.update(open.list.id, name, description, visibility) }, { UiText.Res(R.string.list_saved) })
            else -> Unit
        }
    }

    fun delete(list: ListView) {
        attend()
        deleting = list.id
        launchWrite({ repository.delete(list.id) }, { UiText.Res(R.string.list_deleted, list.name) }, failed = { deleting = null }) {
            effects.post(ListEffect.Deleted(list.id))
        }
    }

    /**
     * [write], then: applied, the dialog closes and [message] says so, then [applied]; failed, the dialog
     * stays with the error; unknown (a create's lost answer), it closes and says so, and the screen's
     * refresh shows what landed. Only the dialog that started it is changed, and only on screen: once
     * dismissed or left, a failure is a message too.
     */
    private fun <T> launchWrite(write: suspend () -> Write<T>, message: (T) -> UiText, failed: () -> Unit = {}, applied: (T) -> Unit = {}) {
        if (busy) {
            failed()
            return
        }
        val open = dialog
        busy = true
        error = null
        val started = account()
        val run = snackbars.scope.async { write() }
        scope.launch {
            try {
                val outcome = run.await()
                val here = presented && dialog === open
                when (outcome) {
                    is Write.Applied -> {
                        outcome.reload?.let(reloaded)
                        if (here) dialog = null
                        say(started, message(outcome.value))
                        applied(outcome.value)
                    }
                    is Write.Failed -> {
                        outcome.reload?.let(reloaded)
                        failed()
                        if (here) error = outcome.text else say(started, outcome.text)
                    }
                    is Write.Unknown -> {
                        reloaded(outcome.reload)
                        if (here) dialog = null
                        say(started, outcome.text)
                    }
                }
            } catch (e: CancellationException) {
                // Cleared with its page while the write ran: nothing here is on screen, so the outcome is a retained message.
                snackbars.later {
                    when (val outcome = run.await()) {
                        is Write.Applied -> message(outcome.value)
                        is Write.Failed -> outcome.text
                        is Write.Unknown -> outcome.text
                    }.takeIf { account() == started }
                }
                throw e
            } finally {
                busy = false
            }
        }
    }

    /** A message: the screen's, while the dialogs' host is composed; else retained, for the covered page may be popped before it shows its effects. */
    private fun say(started: String?, text: UiText) {
        if (presented) effects.post(ListEffect.Message(text)) else snackbars.later { text.takeIf { account() == started } }
    }
}
