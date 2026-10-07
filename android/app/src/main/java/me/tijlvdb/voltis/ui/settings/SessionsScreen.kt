package me.tijlvdb.voltis.ui.settings

import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.DateTimeParseException
import java.time.format.FormatStyle
import javax.inject.Inject
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Session
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.content.CatalogChange
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.user.UserRepository
import me.tijlvdb.voltis.ui.LoadStatus
import me.tijlvdb.voltis.ui.RefreshOnResume
import me.tijlvdb.voltis.ui.Refresher
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.bottomSpace
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.VDialog
import me.tijlvdb.voltis.ui.kit.VIconButton
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VTopBar
import me.tijlvdb.voltis.ui.pageGutter
import me.tijlvdb.voltis.ui.pageGutterBeforeIcon
import me.tijlvdb.voltis.ui.readableWidth

@HiltViewModel
class SessionsViewModel @Inject constructor(private val users: UserRepository, events: CatalogEvents) : ViewModel() {
    var sessions by mutableStateOf<List<Session>?>(null)
        private set

    var error by mutableStateOf<UiText?>(null)
        private set

    var revoking by mutableStateOf(false)
        private set

    /** Why the confirmed sign-out failed. */
    var revokeError by mutableStateOf<UiText?>(null)
        private set

    /** The row that takes over the focus of a revoked one: the next, else the previous. */
    var focus by mutableStateOf<String?>(null)
        private set

    private val _revoked = Channel<Session>(Channel.BUFFERED)
    val revoked = _revoked.receiveAsFlow()

    /** The one reload: a newer one cancels it, so an older answer never brings a revoked row back. */
    private var job: Job? = null

    /** No event changes the list: it is fetched again on a return after a while. */
    val refresher = Refresher(viewModelScope, events, matches = { it is CatalogChange.OnlineChanged }, refresh = ::load)

    init {
        load()
    }

    fun load() {
        job?.cancel()
        error = null
        job = viewModelScope.launch {
            error = attempt {
                val loaded = users.sessions()
                ensureActive()
                sessions = loaded
                if (loaded.none { it.id == focus }) focus = null
            }
        }
    }

    fun revoke(session: Session) {
        if (revoking) return
        revoking = true
        revokeError = null
        viewModelScope.launch {
            revokeError = attempt { users.revokeSession(session.id) }
            revoking = false
            if (revokeError == null) {
                val shown = sessions.orEmpty()
                val rest = shown.filter { it.id != session.id }
                sessions = rest
                focus = (rest.getOrNull(shown.indexOfFirst { it.id == session.id }) ?: rest.lastOrNull())?.id
                _revoked.send(session)
            }
            // Also after a failure: a session revoked elsewhere answers 404, and its row has to go.
            load()
        }
    }

    fun focused() {
        focus = null
    }

    fun clearRevokeError() {
        revokeError = null
    }
}

/** The user's sessions (`SessionsCard.vue`), as rows: any but this one can be signed out. */
@Composable
fun SessionsScreen(onBack: () -> Unit, vm: SessionsViewModel = hiltViewModel()) {
    RefreshOnResume(vm.refresher)
    val snackbars = LocalSnackbars.current
    val resources = LocalResources.current
    val browser = stringResource(R.string.sessions_browser)
    LaunchedEffect(vm, resources) {
        vm.revoked.collect { snackbars.show(resources.getString(R.string.sessions_revoked, it.clientName ?: browser)) }
    }
    var confirming by rememberSaveable { mutableStateOf<String?>(null) }
    Surface(Modifier.fillMaxSize()) {
        Column {
            VTopBar(stringResource(R.string.sessions_title), onBack)
            Box(Modifier.readableWidth(pageGutter()).padding(horizontal = pageGutter())) { LoadStatus(vm.sessions == null, vm.error, vm::load) }
            val sessions = vm.sessions ?: return@Column
            LazyColumn(contentPadding = PaddingValues(bottom = bottomSpace())) {
                items(sessions, key = { it.id }) { session ->
                    Column(Modifier.readableWidth(pageGutter()).padding(start = pageGutter(), end = pageGutterBeforeIcon())) {
                        SessionRow(session, focus = vm.focus == session.id, vm::focused) {
                            vm.clearRevokeError()
                            confirming = session.id
                        }
                        HorizontalDivider(Modifier.padding(end = 12.dp), color = MaterialTheme.colorScheme.outlineVariant)
                    }
                }
            }
        }
    }
    // A revoked session leaves the list, which closes its dialog.
    val session = vm.sessions?.find { it.id == confirming } ?: return
    VDialog(
        stringResource(R.string.sessions_revoke_title),
        stringResource(R.string.settings_sign_out),
        onConfirm = { vm.revoke(session) },
        onDismiss = {
            confirming = null
            vm.clearRevokeError()
        },
        text = stringResource(R.string.sessions_revoke_confirm, fullLabel(session)),
        danger = true,
        busy = vm.revoking,
        error = vm.revokeError,
    )
}

@Composable
private fun SessionRow(session: Session, focus: Boolean, onFocused: () -> Unit, onRevoke: () -> Unit) {
    val colors = MaterialTheme.colorScheme
    val small = MaterialTheme.typography.bodySmall
    val requester = remember { FocusRequester() }
    if (focus) {
        val window = LocalWindowInfo.current
        LaunchedEffect(Unit) {
            // The confirmation's window is still closing, and TalkBack then settles on the screen
            // under it by itself: a focus moved before that is lost.
            snapshotFlow { window.isWindowFocused }.first { it }
            delay(REFOCUS_DELAY_MS)
            requester.requestFocus()
            onFocused()
        }
    }
    Row(
        // Focusable, so a keyboard's or TalkBack's focus has a row to land on after a sign-out.
        Modifier.fillMaxWidth().heightIn(min = 72.dp).focusRequester(requester).focusable().semantics(mergeDescendants = true) {},
        Arrangement.spacedBy(12.dp),
        Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f).padding(vertical = 10.dp)) {
            Text(session.clientName ?: stringResource(R.string.sessions_browser), style = MaterialTheme.typography.bodyLarge)
            Text(stringResource(R.string.sessions_signed_in, localDate(session.createdAt)), color = colors.onSurfaceVariant, style = small)
            val method = methodLabel(session.method)
            Text(
                session.lastUsedAt?.let { stringResource(R.string.sessions_last_used, method, localDate(it)) } ?: method,
                color = colors.onSurfaceVariant,
                style = small,
            )
        }
        if (session.current) {
            Text(stringResource(R.string.sessions_current), Modifier.padding(end = 12.dp), color = colors.onSurfaceVariant, style = small)
        } else {
            VIconButton(VIcons.Logout, stringResource(R.string.sessions_revoke, fullLabel(session)), onRevoke)
        }
    }
}

private const val REFOCUS_DELAY_MS = 700L

/** With the sign-in date, which tells browser rows apart. */
@Composable
private fun fullLabel(session: Session) = stringResource(
    R.string.sessions_full_label,
    session.clientName ?: stringResource(R.string.sessions_browser),
    localDate(session.createdAt),
)

/** A server timestamp's date here, as the web's `toLocaleDateString`. */
private fun localDate(value: String): String = try {
    OffsetDateTime.parse(value).atZoneSameInstant(ZoneId.systemDefault()).format(DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM))
} catch (_: DateTimeParseException) {
    value
}
