package me.tijlvdb.voltis.ui

import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource

/** Text for the user: a string resource, or a message from the server. */
sealed interface UiText {
    data class Res(@param:StringRes val id: Int, val arg: Any? = null) : UiText

    data class Raw(val text: String) : UiText

    @Composable
    fun resolve(): String = when (this) {
        is Res -> if (arg == null) stringResource(id) else stringResource(id, arg)
        is Raw -> text
    }
}
