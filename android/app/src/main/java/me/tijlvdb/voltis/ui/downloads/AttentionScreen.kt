package me.tijlvdb.voltis.ui.downloads

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.attempt
import me.tijlvdb.voltis.data.auth.SessionStore
import me.tijlvdb.voltis.data.sync.SyncCenter
import me.tijlvdb.voltis.domain.reading.AttentionItem
import me.tijlvdb.voltis.domain.reading.ReadingSync
import me.tijlvdb.voltis.domain.sync.UNREADABLE_ERROR
import me.tijlvdb.voltis.ui.OfflineBar
import me.tijlvdb.voltis.ui.needsConnection
import me.tijlvdb.voltis.ui.EndReviewOnStop
import me.tijlvdb.voltis.ui.Reviewer
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.noticeText
import me.tijlvdb.voltis.ui.bottomSpace
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.PageHeader
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VDialog
import me.tijlvdb.voltis.ui.kit.VDisabled
import me.tijlvdb.voltis.ui.kit.VSpinner
import me.tijlvdb.voltis.ui.kit.VTopBar
import me.tijlvdb.voltis.ui.pageGutter
import me.tijlvdb.voltis.ui.readableWidth
import me.tijlvdb.voltis.ui.reader.SyncPromptDialog

@HiltViewModel
class AttentionViewModel @Inject constructor(private val sync: ReadingSync, private val center: SyncCenter, session: SessionStore) : ViewModel() {
    /** The account the screen was opened for: the stack is cleared when it changes. */
    private val account = session.state.value.account

    /** Null until read. */
    val items = (account?.let(center::attention) ?: flowOf(emptyList())).stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), null)

    private val _messages = Channel<UiText>(Channel.BUFFERED)
    val messages = _messages.receiveAsFlow()

    val reviewer = Reviewer(viewModelScope, sync, account) { _messages.trySend(it) }

    fun review(item: AttentionItem.Held) = reviewer.start(item.contentId, item.title)

    /** The stuck change whose Discard asks for a confirmation. */
    var discarding by mutableStateOf<AttentionItem.Unsynced?>(null)
        private set

    fun askDiscard(item: AttentionItem.Unsynced) {
        discarding = item
    }

    fun cancelDiscard() {
        discarding = null
    }

    fun retry(item: AttentionItem.Unsynced) {
        viewModelScope.launch { attempt { center.retry(checkNotNull(account), item.contentId) }?.let { _messages.trySend(it) } }
    }

    fun discard(item: AttentionItem.Unsynced) {
        discarding = null
        viewModelScope.launch { attempt { center.discard(checkNotNull(account), item.contentId) }?.let { _messages.trySend(it) } }
    }

    fun dismiss(id: Long) {
        viewModelScope.launch { attempt { sync.dismiss(checkNotNull(account), id) }?.let { _messages.trySend(it) } }
    }
}

/**
 * "Needs attention" (P2 §11): items changed on another device while this one held unsent reading,
 * answered here with the reader's dialog, and notices of changes that weren't applied.
 */
@Composable
fun AttentionScreen(onBack: () -> Unit, vm: AttentionViewModel = hiltViewModel()) {
    val items by vm.items.collectAsStateWithLifecycle()
    val snackbars = LocalSnackbars.current
    val context = LocalContext.current
    LaunchedEffect(vm) { vm.messages.collect { snackbars.show(it.string(context)) } }
    val title = stringResource(R.string.attention_title)
    val state = rememberLazyListState()
    val scrolled by remember { derivedStateOf { state.firstVisibleItemIndex > 0 } }
    Surface(Modifier.fillMaxSize()) {
        Column {
            VTopBar(if (scrolled) title else "", onBack)
            LazyColumn(Modifier.fillMaxSize(), state, contentPadding = PaddingValues(bottom = bottomSpace())) {
                item(key = "header") {
                    Column(Modifier.readableWidth(pageGutter()).padding(horizontal = pageGutter()).padding(bottom = 8.dp), Arrangement.spacedBy(8.dp)) {
                        PageHeader(title)
                        OfflineBar()
                    }
                }
                val current = items
                when {
                    current == null -> item(key = "loading") { Column(Modifier.readableWidth(pageGutter()).padding(horizontal = pageGutter(), vertical = 16.dp)) { VSpinner() } }
                    current.isEmpty() -> item(key = "empty") {
                        Text(stringResource(R.string.attention_empty), Modifier.readableWidth(pageGutter()).padding(horizontal = pageGutter(), vertical = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    else -> items(current, key = { it.key }) { item ->
                        when (item) {
                            is AttentionItem.Held -> {
                                // Offline it can't read the server's state.
                                val reason = needsConnection()
                                val review = stringResource(R.string.sync_review_named, item.title)
                                AttentionRow(item.title, stringResource(R.string.sync_changed_elsewhere), reason, pageGutter()) {
                                    VDisabled(reason, review) { enabled -> VButton(stringResource(R.string.sync_review), { vm.review(item) }, style = VButtonStyle.Tonal, enabled = enabled, description = review) }
                                }
                            }
                            is AttentionItem.Unsynced -> {
                                // A retry sends it at once.
                                val reason = needsConnection()
                                val retry = stringResource(R.string.downloads_retry_named, item.title)
                                AttentionRow(stringResource(R.string.attention_unsynced, item.title), when (item.lastError) {
                                    null -> stringResource(R.string.attention_unsynced_text)
                                    UNREADABLE_ERROR -> stringResource(R.string.error_unexpected_response)
                                    else -> item.lastError
                                }, reason) {
                                    Row {
                                        VDisabled(reason, retry) { enabled -> VButton(stringResource(R.string.retry), { vm.retry(item) }, style = VButtonStyle.Tonal, enabled = enabled, description = retry) }
                                        VButton(stringResource(R.string.attention_discard), { vm.askDiscard(item) }, style = VButtonStyle.Text, description = stringResource(R.string.attention_discard_named, item.title))
                                    }
                                }
                            }
                            is AttentionItem.Notice -> AttentionRow(item.title, noticeText(item.kind, item.detail).resolve()) {
                                VButton(stringResource(R.string.attention_dismiss), { vm.dismiss(item.id) }, style = VButtonStyle.Text, description = stringResource(R.string.attention_dismiss_named, item.title))
                            }
                        }
                    }
                }
            }
        }
    }
    vm.discarding?.let { item ->
        VDialog(
            stringResource(R.string.attention_discard_title),
            stringResource(R.string.attention_discard),
            { vm.discard(item) },
            vm::cancelDiscard,
            text = stringResource(R.string.attention_discard_text, item.title),
            danger = true,
        )
    }
    EndReviewOnStop(vm.reviewer)
    vm.reviewer.current?.let { SyncPromptDialog(it.session, it.title, ContentType.COMIC) }
}

private val AttentionItem.key get() = when (this) {
    is AttentionItem.Held -> "h:$contentId"
    is AttentionItem.Notice -> "n:$id"
    is AttentionItem.Unsynced -> "u:$contentId"
}

/** An item's title and what is wrong with it, read together, then what can be done. [note] is shown only: the action says it. The last button's visible edge is on the gutter: [end] is 12 dp short of it for a text button, whose label has that much padding, and the gutter itself for a filled one. */
@Composable
private fun AttentionRow(title: String, text: String, note: String? = null, end: Dp = pageGutter() - 12.dp, actions: @Composable () -> Unit) {
    Row(
        Modifier.readableWidth(pageGutter()).padding(start = pageGutter(), end = end).heightIn(min = 72.dp),
        Arrangement.spacedBy(12.dp),
        Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f).padding(vertical = 8.dp).semantics(mergeDescendants = true) {}) {
            Text(title, style = MaterialTheme.typography.bodyLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text(text, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
            if (note != null) Text(note, Modifier.clearAndSetSemantics {}, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
        }
        actions()
    }
}
