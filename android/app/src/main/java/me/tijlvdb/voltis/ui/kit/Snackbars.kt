package me.tijlvdb.voltis.ui.kit

import android.content.Context
import android.os.SystemClock
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.SnackbarVisuals
import androidx.compose.runtime.mutableStateSetOf
import androidx.compose.runtime.snapshotFlow
import androidx.compose.runtime.staticCompositionLocalOf
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.ActivityRetainedLifecycle
import dagger.hilt.android.components.ActivityComponent
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.android.scopes.ActivityRetainedScoped
import javax.inject.Inject
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Job
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.job
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.ui.UiText

/** Marks one message, for [Snackbars.showing]. Its own type, so nothing else (a lambda) can be passed as a key. */
class SnackbarKey

/** The snackbars' state, kept across configuration changes: the Activity's. A view model may hold it. */
@ActivityRetainedScoped
class SnackbarsModel internal constructor(
    private val resolve: (UiText) -> String,
    /** Outlives every page. A launch into it starts at once. */
    val scope: CoroutineScope,
    private val now: () -> Long,
) {
    @Inject
    constructor(@ApplicationContext context: Context, lifecycle: ActivityRetainedLifecycle) : this(
        { it.string(context) }, CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate), SystemClock::elapsedRealtime,
    ) {
        lifecycle.addOnClearedListener { scope.cancel() }
    }

    val state = SnackbarHostState()

    /** Keys of messages queued or showing. */
    val keys = mutableStateSetOf<SnackbarKey>()

    /** Action snackbars that belong to the reader; it cancels them when it goes. */
    private val retained = mutableSetOf<Job>()

    fun track(job: Job) {
        retained += job
        job.invokeOnCompletion { retained -= job }
    }

    fun cancelRetained() = retained.toList().forEach { it.cancel() }

    /** A message without an action, and when ([now]) it was first seen on screen: null while it waits. */
    private class Plain(val text: String) : SnackbarVisuals {
        override val message get() = text
        override val actionLabel: String? = null
        override val withDismissAction = false
        override val duration = SnackbarDuration.Short
        var shownAt: Long? = null
        var job: Job? = null
    }

    /** The newest message that hasn't been shown yet: a newer one supersedes it. */
    private var pending: Plain? = null

    /** The message on screen that [show] may replace. */
    private var visible: Plain? = null

    /** The host's current snackbar, as ours: the one on screen is no longer pending, and its time starts. */
    private fun sync() {
        val current = state.currentSnackbarData?.visuals as? Plain
        if (current != null && current.shownAt == null) {
            current.shownAt = now()
            if (pending === current) pending = null
        }
        visible = current
    }

    // The host's changes, seen as they happen; [sync] also runs before a message is judged, so none is missed.
    private val observer = scope.launch { snapshotFlow { state.currentSnackbarData }.collect { sync() } }

    /**
     * A message without an action. It waits for an action snackbar showing or queued, but not for a
     * plain message: that one is replaced once it has been visible for [MIN_VISIBLE_MS], and a
     * message still waiting is dropped for this newer one. [key] marks it for [Snackbars.showing].
     */
    fun show(text: String, key: SnackbarKey? = null) {
        key?.let { keys += it }
        sync()
        // Never one that owns the host: [sync] took it out of pending.
        pending?.job?.cancel()
        val plain = Plain(text)
        pending = plain
        // Undispatched: queued behind what is there when this returns, in arrival order with the actions.
        plain.job = scope.launch(start = CoroutineStart.UNDISPATCHED) {
            // The plain message showing makes way for this one once it has had its time.
            val replacing = visible?.let { old ->
                launch {
                    val hold = (old.shownAt ?: now()) + MIN_VISIBLE_MS - now()
                    if (hold > 0) delay(hold)
                    state.currentSnackbarData?.takeIf { it.visuals === old }?.dismiss()
                }
            }
            try {
                state.showSnackbar(plain)
            } finally {
                replacing?.cancel()
                if (visible === plain) visible = null
                if (pending === plain) pending = null
                key?.let { keys -= it }
            }
        }
    }

    /** Shows what [message] answers, unless null: for a write whose page may be gone before it ends. */
    fun later(message: suspend () -> UiText?) {
        scope.launch { message()?.let { show(resolve(it)) } }
    }

    /** The signed-in app left (a session change, or the activity finishing): nothing queued here is for what comes next. */
    fun drop() {
        scope.coroutineContext.job.children.filter { it !== observer }.forEach { it.cancel() }
    }
}

private const val MIN_VISIBLE_MS = 1_000L

/** Where a composable finds the [SnackbarsModel] a view model is given. */
@EntryPoint
@InstallIn(ActivityComponent::class)
interface SnackbarsEntryPoint {
    fun snackbars(): SnackbarsModel
}

/**
 * The signed-in app's snackbars, shown above the navigation bar. A message without an action belongs
 * to [model], so it is queued once [show] returns and a rotation neither drops nor repeats it. One
 * with an action belongs to the scaffold's [composition], which a rotation cancels with its callback,
 * unless it is [retained]: then its callback may not capture anything a rotation replaces.
 */
class Snackbars(private val model: SnackbarsModel, private val composition: CoroutineScope) {
    val state get() = model.state

    /**
     * It outlives the screen that asked: the reader may open over it. A message waits for the one
     * showing. [action] is a button such as Undo or Retry, run by [onAction]; an action snackbar
     * never replaces another, and waits its turn in arrival order. [key] marks a message for
     * [showing]. A [retained] one lives in [model] like a plain message, so a rotation keeps it and
     * its action runs once. [onAction] stays last: it is the callers' trailing lambda.
     */
    fun show(text: String, action: String? = null, key: SnackbarKey? = null, retained: Boolean = false, onAction: () -> Unit = {}) {
        if (action == null) {
            model.show(text, key)
            return
        }
        // Undispatched: it is in Material's queue when this returns, so arrival order holds across the two scopes.
        val job = (if (retained) model.scope else composition).launch(start = CoroutineStart.UNDISPATCHED) {
            // With an action Material's default is to stay until dismissed.
            if (state.showSnackbar(text, action, duration = SnackbarDuration.Long) == SnackbarResult.ActionPerformed) onAction()
        }
        if (retained) model.track(job)
    }

    /** Takes down the [retained] action snackbars, queued or showing: their callbacks belong to a screen that left. */
    fun cancelRetained() = model.cancelRetained()

    /** The message shown with [key] is queued or on screen. Snapshot state. */
    fun showing(key: SnackbarKey) = key in model.keys
}

val LocalSnackbars = staticCompositionLocalOf<Snackbars> { error("No snackbar host") }
