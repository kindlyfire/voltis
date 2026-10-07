package me.tijlvdb.voltis.ui.kit

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.onClick
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.stateDescription

/**
 * A control that is disabled for a [reason], such as "Needs a connection" (P2 §10). [content] is
 * composed with `enabled = false`, and the box around it is one TalkBack stop: "[label], [state],
 * [reason]", where [state] is what the control would say of itself (a toggle's on or off, a
 * rating's value). A tap says the reason in a snackbar. Without a reason, [content] is enabled and
 * speaks for itself. The structure is the same either way, so the control keeps its state and focus
 * when the reason comes and goes.
 *
 * An [anchor] goes on the box while disabled, and on the enabled [content] otherwise (the box hides
 * what is inside it), so a popover can return focus to whichever is exposed.
 *
 * The disabled control's own semantics can't be merged in instead: a clickable or toggleable is a
 * TalkBack stop of its own, which never says the reason.
 */
@Composable
fun VDisabled(reason: String?, label: String, modifier: Modifier = Modifier, state: String? = null, anchor: PopoverAnchor? = null, content: @Composable (enabled: Boolean) -> Unit) {
    val disabled = if (reason == null) {
        Modifier
    } else {
        // Only here: an enabled control works without a snackbar host (the reader).
        val snackbars = LocalSnackbars.current
        // The disabled content doesn't take the tap, so it reaches this box.
        Modifier
            .clickable { snackbars.show(reason) }
            .clearAndSetSemantics {
                contentDescription = label
                stateDescription = listOfNotNull(state, reason).joinToString(", ")
                role = Role.Button
                if (anchor != null) popoverAnchor(anchor)
                onClick {
                    snackbars.show(reason)
                    true
                }
            }
    }
    Box(modifier.then(disabled)) { content(reason == null) }
}
