package me.tijlvdb.voltis.ui

import android.content.Context
import androidx.annotation.PluralsRes
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources

/** Text for the user: a string resource, or a message from the server. */
sealed interface UiText {
    data class Res(@param:StringRes val id: Int, val arg: Any? = null) : UiText

    /** A counted text: "Rated 3 stars". */
    data class Plural(@param:PluralsRes val id: Int, val count: Int) : UiText

    data class Raw(val text: String) : UiText

    /** A text with several [args]; one that is itself a [UiText] is resolved first. */
    data class Format(@param:StringRes val id: Int, val args: List<Any>) : UiText

    @Composable
    fun resolve(): String {
        // Read so a configuration change recomposes.
        LocalResources.current
        return string(LocalContext.current)
    }

    /** Outside composition: a snackbar's text. */
    fun string(context: Context): String = when (this) {
        is Res -> if (arg == null) context.getString(id) else context.getString(id, arg)
        is Plural -> context.resources.getQuantityString(id, count, count)
        is Raw -> text
        is Format -> context.getString(id, *args.map { if (it is UiText) it.string(context) else it }.toTypedArray())
    }
}
