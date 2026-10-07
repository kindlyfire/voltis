package me.tijlvdb.voltis.ui.lists

import android.view.ViewTreeObserver
import androidx.activity.compose.LocalActivity
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.State
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onPlaced
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.isTraversalGroup
import androidx.compose.ui.semantics.traversalIndex
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.flow.first
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.content.coverUrl
import me.tijlvdb.voltis.data.lists.EntryView
import me.tijlvdb.voltis.data.lists.Place
import me.tijlvdb.voltis.data.lists.ListView
import me.tijlvdb.voltis.ui.EffectHost
import me.tijlvdb.voltis.ui.RefreshOnResume
import me.tijlvdb.voltis.ui.OfflineBar
import me.tijlvdb.voltis.ui.ServerOffline
import me.tijlvdb.voltis.ui.needsConnection
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.bottomSpace
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.PageHeader
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VCard
import me.tijlvdb.voltis.ui.kit.VCover
import me.tijlvdb.voltis.ui.nav.HeroKey
import me.tijlvdb.voltis.ui.nav.LocalHeroOrigin
import me.tijlvdb.voltis.ui.nav.LocalHeroTaps
import me.tijlvdb.voltis.ui.kit.VDisabled
import me.tijlvdb.voltis.ui.kit.VIconButton
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VLinkChip
import me.tijlvdb.voltis.ui.kit.VMenuItem
import me.tijlvdb.voltis.ui.kit.VOverflowMenu
import me.tijlvdb.voltis.ui.kit.VSpinner
import me.tijlvdb.voltis.ui.kit.VTag
import me.tijlvdb.voltis.ui.kit.VTopBar
import me.tijlvdb.voltis.ui.pageGutter
import me.tijlvdb.voltis.ui.rememberAnnouncer
import me.tijlvdb.voltis.ui.rememberTouchExploration
import me.tijlvdb.voltis.ui.typeLabel
import me.tijlvdb.voltis.ui.wideReadableWidth

/** A list's page (the web's `ListPage.vue`) from the cache. Its edits need the server. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ListScreen(
    id: String,
    onBack: () -> Unit,
    openContent: (String) -> Unit,
    openLibrary: (String) -> Unit,
    vm: ListViewModel = hiltViewModel<ListViewModel, ListViewModel.Factory> { it.create(id) },
) {
    RefreshOnResume(vm.refresher)
    val cachedState = vm.cached.collectAsStateWithLifecycle()
    val cached by cachedState
    val detail = cached?.detail
    val storeFailed by vm.storeFailed.collectAsStateWithLifecycle()
    val online by vm.online.collectAsStateWithLifecycle()
    val exploring = rememberTouchExploration()
    val snackbars = LocalSnackbars.current
    val context = LocalContext.current
    val announcer = rememberAnnouncer()
    WatchAttention(vm::attend)
    val back = {
        vm.attend()
        onBack()
    }
    val retry = {
        vm.attend()
        vm.refresh(pulled = false)
    }
    val state = rememberLazyListState()
    val taps = LocalHeroTaps.current
    val origin = LocalHeroOrigin.current
    // Each row's and the title's (null) focus anchor, once placed.
    val anchors = remember { mutableStateMapOf<String?, FocusRequester>() }
    val ports = remember(state) { Ports(state, anchors) { cachedState.value?.detail?.entries.orEmpty() } }
    // Focus follows a write only under TalkBack, and only while the user did nothing since (P4 §11).
    val listPresent = { cachedState.value?.detail != null }
    val displayedRevision = { cachedState.value?.detail?.revision ?: 0L }
    val valid = { intent: FocusIntent? -> intent != null && vm.attention == intent.attention && exploring.value }
    EffectHost(vm.effects) { effect ->
        when (effect) {
            is ListEffect.Message -> snackbars.show(effect.text.string(context), key = effect.key)
            is ListEffect.Announce -> announcer.say(effect.text.string(context), vm.attention)
            is ListEffect.Moved -> {
                // A row moved out of view loses TalkBack's focus (the spike): it is scrolled to and given focus.
                // The displayed rows may still be the move's prefetch: the handover waits for the reload's place.
                handOver(effect.focus, valid, ports) { shown -> movedTarget(effect.entryId, effect.revision, displayedRevision(), listPresent(), shown) }
                // Not for a row that went meanwhile. The verified place, unless the displayed rows are of a newer refresh: then theirs.
                val shown = ports.displayed
                val at = shown.indexOf(effect.entryId)
                if (at >= 0) {
                    val place = if (displayedRevision() > effect.revision) Place(at, shown.size) else effect.place
                    announcer.say(UiText.Format(R.string.entry_moved, listOf(place.index + 1, place.of)).string(context), vm.attention)
                }
            }
            is ListEffect.Removed -> {
                // A snackbar appearing moves TalkBack to the top (the spike): focus is handed over once the message has gone.
                snapshotFlow { !valid(effect.focus) || !snackbars.showing(effect.after) }.first { it }
                handOver(effect.focus, valid, ports) { shown -> removedTarget(effect.gone, effect.order, effect.revision, displayedRevision(), listPresent(), shown) }
            }
            is ListEffect.Deleted -> if (effect.listId == id) onBack()
        }
    }
    val scrolled by remember { derivedStateOf { state.firstVisibleItemIndex > 0 } }
    Surface(Modifier.fillMaxSize()) {
        Column(
            Modifier
                // Touches, TalkBack's hovers and keys: the user went elsewhere.
                .pointerInput(Unit) {
                    awaitPointerEventScope {
                        while (true) {
                            awaitPointerEvent(PointerEventPass.Initial)
                            vm.attend()
                        }
                    }
                }
                .onPreviewKeyEvent {
                    vm.attend()
                    false
                },
        ) {
            VTopBar(if (scrolled) detail?.list?.name.orEmpty() else "", back)
            announcer.Regions(vm.attention)
            // The web's ListPage is a 960 px page.
            val padded = Modifier.wideReadableWidth().padding(horizontal = pageGutter())
            when {
                vm.editor.deleting == id -> Unit
                detail != null -> PullToRefreshBox(vm.refreshing, { vm.refresh() }) {
                    LazyColumn(Modifier.fillMaxSize(), state, PaddingValues(bottom = bottomSpace()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        item(key = HEADER, contentType = "header") {
                            Header(detail.list, vm, exploring.value, padded, Modifier.anchor(anchors, null, rememberAnchor(anchors, null)), retry)
                        }
                        if (detail.entries.isEmpty()) {
                            item(key = "empty", contentType = "empty") {
                                Text(stringResource(R.string.list_no_entries), padded.padding(vertical = 12.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                        val last = detail.entries.lastIndex
                        itemsIndexed(detail.entries, key = { _, it -> it.entryId }, contentType = { _, _ -> "entry" }) { i, entry ->
                            EntryCard(
                                entry,
                                vm.libraries?.get(entry.libraryId),
                                entryActions(entry, vm, online, first = i == 0, last = i == last, exploring),
                                online,
                                // Under TalkBack the card is focusable in touch mode too, so focus can be handed
                                // to it, which TalkBack follows. Otherwise the card's own clickable is the one stop.
                                padded,
                                Modifier.anchor(anchors, entry.entryId, rememberAnchor(anchors, entry.entryId)).then(if (exploring.value) Modifier.focusable() else Modifier),
                                {
                                    vm.attend()
                                    taps.tap(HeroKey(entry.contentId, origin + "/entry"))
                                    openContent(entry.contentId)
                                },
                                {
                                    vm.attend()
                                    openLibrary(entry.libraryId)
                                },
                            )
                        }
                    }
                }
                storeFailed != null -> QueryError(UiText.Res(R.string.error_offline_data), padded.padding(top = 24.dp))
                // Gone only on the server's word: a refresh's "List not found", or a list that went from the open store.
                cached?.removed == true || vm.gone -> Gone(back, padded)
                vm.busy -> Spinner(padded)
                vm.error != null -> QueryError(vm.error, padded.padding(top = 24.dp), retry)
                // Not cached, and the server out of reach.
                vm.offline -> ServerOffline(onRetry = retry)
                // The cache isn't read yet, or no account store is open yet (cached?.open == false).
                else -> Spinner(padded)
            }
        }
    }
    ListDialogs(vm.editor, online)
    Presented(vm::presentNotes, vm::leaveNotes)
    vm.notesFor?.let { NotesDialog(it, online, vm.writing, vm.notesError, vm::saveNotes) { vm.editNotes(null) } }
}

/** The header item's key in the lazy list. */
private const val HEADER = "header"

/** [id]'s focus anchor, which [anchor] registers in [anchors] once placed; removed when it leaves. */
@Composable
private fun rememberAnchor(anchors: MutableMap<String?, FocusRequester>, id: String?): FocusRequester {
    val focus = remember { FocusRequester() }
    DisposableEffect(id) { onDispose { if (anchors[id] === focus) anchors.remove(id) } }
    return focus
}

private fun Modifier.anchor(anchors: MutableMap<String?, FocusRequester>, id: String?, focus: FocusRequester) =
    focusRequester(focus).onPlaced { if (anchors[id] !== focus) anchors[id] = focus }

/** The page as [handOver] sees it: the displayed rows after the header (item 0), and the list's layout, read by item key. */
private class Ports(
    private val state: LazyListState,
    private val anchors: Map<String?, FocusRequester>,
    private val entries: () -> List<EntryView>,
) : HandoverPorts {
    override val displayed get() = entries().map { it.entryId }

    override fun lazyIndex(target: String?) = if (target == null) 0 else 1 + displayed.indexOf(target)

    /** [target]'s item where the layout has it now, which may be an earlier list's place. */
    private fun item(target: String?) = state.layoutInfo.visibleItemsInfo.find { it.key == (target ?: HEADER) }

    override fun lagging(target: String?) = item(target)?.let { it.index != lazyIndex(target) } ?: false

    override fun fullyVisible(target: String?): Boolean {
        val info = state.layoutInfo
        val item = item(target) ?: return false
        return item.offset >= 0 && item.offset + item.size <= info.viewportEndOffset - info.afterContentPadding
    }

    override fun partlyVisible(target: String?): Boolean {
        val info = state.layoutInfo
        val item = item(target) ?: return false
        return item.offset < info.viewportEndOffset - info.afterContentPadding && item.offset + item.size > 0
    }

    override fun anchored(target: String?) = target in anchors

    override fun requestFocus(target: String?) {
        // Best effort: a node that detached since it was placed can't take focus.
        runCatching { anchors[target]?.requestFocus() }
    }

    override suspend fun scrollTo(index: Int) = state.scrollToItem(index)
}

/**
 * Bumps attention when the page pauses or loses window focus (a shade, a dialog, another window), and
 * when it leaves; a rotation is none of these. Observed through callbacks, not sampled.
 */
@Composable
private fun WatchAttention(attend: () -> Unit) {
    val activity = LocalActivity.current
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val view = LocalView.current
    val current by rememberUpdatedState(attend)
    DisposableEffect(lifecycle, view) {
        val changing = { activity?.isChangingConfigurations == true }
        val paused = LifecycleEventObserver { _, event -> if (event == Lifecycle.Event.ON_PAUSE && !changing()) current() }
        val focus = ViewTreeObserver.OnWindowFocusChangeListener { if (!it && !changing()) current() }
        lifecycle.addObserver(paused)
        view.viewTreeObserver.addOnWindowFocusChangeListener(focus)
        onDispose {
            lifecycle.removeObserver(paused)
            view.viewTreeObserver.removeOnWindowFocusChangeListener(focus)
            if (!changing()) current()
        }
    }
}

/** A row's menu, and the accessibility actions ([spoken]) offered with it. */
private class RowActions(val menu: List<VMenuItem>, val spoken: List<VMenuItem>)

/**
 * The row's menu items and, from the same flags, its accessibility actions: a disabled item isn't
 * offered, except a move at an end, which is refused with "Already first" or "Already last" (P4 §11).
 * Offline there are none to act.
 */
@Composable
private fun entryActions(entry: EntryView, vm: ListViewModel, online: Boolean, first: Boolean, last: Boolean, exploring: State<Boolean>): RowActions {
    val enabled = online && !vm.writing
    val moves = enabled && !vm.movesBlocked
    val edit = VMenuItem(stringResource(R.string.entry_edit_notes), enabled) { vm.editNotes(entry) }
    val up = VMenuItem(stringResource(R.string.entry_move_up), moves && !first) { vm.move(entry, -1, exploring.value) }
    val down = VMenuItem(stringResource(R.string.entry_move_down), moves && !last) { vm.move(entry, 1, exploring.value) }
    val remove = VMenuItem(stringResource(R.string.entry_remove), enabled, danger = true) { vm.remove(entry, exploring.value) }
    return RowActions(
        listOf(edit, up, down, remove),
        listOfNotNull(edit.takeIf { it.enabled }, up.takeIf { it.enabled || first }, down.takeIf { it.enabled || last }, remove.takeIf { it.enabled }),
    )
}

/** Under TalkBack ([exploring]) the title is focusable, with [anchor], for the focus after the last row is removed. */
@Composable
private fun Header(list: ListView, vm: ListViewModel, exploring: Boolean, modifier: Modifier, anchor: Modifier, retry: () -> Unit) {
    Column(modifier, Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.Top) {
            PageHeader(list.name, Modifier.weight(1f).then(anchor).then(if (exploring) Modifier.focusable() else Modifier))
            val edit = stringResource(R.string.list_edit)
            VDisabled(needsConnection(), edit, Modifier.padding(top = 8.dp)) { enabled ->
                VIconButton(VIcons.Edit, edit, { vm.editor.show(ListDialog.Edit(list)) }, enabled = enabled)
            }
        }
        OfflineBar()
        Text(
            stringResource(R.string.list_meta, visibilityLabel(list.visibility), entriesLabel(list), updatedLabel(list)),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodyMedium,
        )
        // Text keeps the description's line breaks.
        list.description?.takeIf { it.isNotBlank() }?.let { Text(it, style = MaterialTheme.typography.bodyLarge) }
        QueryError(vm.error, retry = retry)
    }
}

/** The card opens the content; the library chip, when the libraries are known, opens the library. */
@Composable
private fun EntryCard(entry: EntryView, library: String?, actions: RowActions, online: Boolean, modifier: Modifier, card: Modifier, onClick: () -> Unit, openLibrary: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val custom = if (online) actions.spoken.map { CustomAccessibilityAction(it.label) { it.onClick(); true } } else emptyList()
    // The menu button lies over the card instead of inside it, so a screen reader reads the card first, then its menu.
    Box(modifier.semantics { isTraversalGroup = true }) {
        VCard(onClick, card.semantics { customActions = custom; traversalIndex = 0f }) {
            VCover(coverUrl(entry.contentId, entry.coverVersion), Modifier.width(72.dp), HeroKey(entry.contentId, LocalHeroOrigin.current + "/entry"))
            Column(Modifier.weight(1f), Arrangement.spacedBy(6.dp)) {
                Text(entry.title, style = MaterialTheme.typography.titleMedium, maxLines = 2, overflow = TextOverflow.Ellipsis)
                FlowRow(Modifier.fillMaxWidth(), Arrangement.spacedBy(8.dp), Arrangement.spacedBy(4.dp), itemVerticalAlignment = Alignment.CenterVertically) {
                    entry.type?.let { VTag(typeLabel(it), onCard = true) }
                    library?.let { VLinkChip(it, openLibrary) }
                }
                entry.notes?.takeIf { it.isNotBlank() }?.let { notes ->
                    Text(stringResource(R.string.list_notes), color = muted, style = MaterialTheme.typography.labelMedium)
                    Text(notes, style = MaterialTheme.typography.bodyMedium)
                }
            }
            // Room for the menu button, which lies over this end.
            Spacer(Modifier.width(48.dp))
        }
        val options = stringResource(R.string.entry_options, entry.title)
        Box(Modifier.align(Alignment.TopEnd).padding(top = 12.dp, end = 12.dp).semantics { traversalIndex = 1f }) {
            VDisabled(needsConnection(), options) { enabled -> VOverflowMenu(actions.menu, enabled = enabled, label = options) }
        }
    }
}

@Composable
private fun Spinner(modifier: Modifier) {
    Column(modifier.padding(top = 24.dp), horizontalAlignment = Alignment.CenterHorizontally) { VSpinner() }
}

@Composable
private fun Gone(onBack: () -> Unit, modifier: Modifier) {
    Column(modifier.padding(top = 48.dp), Arrangement.spacedBy(16.dp), Alignment.CenterHorizontally) {
        Text(stringResource(R.string.list_gone), Modifier.semantics { heading() }, style = MaterialTheme.typography.headlineMedium)
        VButton(stringResource(R.string.back), onBack)
    }
}
