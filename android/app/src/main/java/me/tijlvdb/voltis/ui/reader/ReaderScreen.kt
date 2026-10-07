package me.tijlvdb.voltis.ui.reader

import androidx.activity.compose.LocalActivity
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.PointerInputScope
import androidx.compose.ui.input.pointer.changedToUp
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.res.stringResource
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.ui.isLarge
import me.tijlvdb.voltis.ui.kit.PopoverAnchor
import me.tijlvdb.voltis.ui.kit.VSidePanel
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.LifecycleResumeEffect
import kotlinx.coroutines.flow.MutableSharedFlow
import me.tijlvdb.voltis.domain.comic.ReaderMode
import me.tijlvdb.voltis.domain.comic.ReadingDirection
import me.tijlvdb.voltis.domain.catalog.readerTitle
import me.tijlvdb.voltis.ui.LoadStatus
import me.tijlvdb.voltis.ui.itemLabels
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.touchExploration

/**
 * [onVolume] replaces this reader with another volume's. [onClose] leaves it, for the page of the
 * given content when there is one.
 */
@Composable
fun ReaderScreen(
    contentId: String,
    onVolume: (String) -> Unit,
    onClose: (contentId: String?) -> Unit,
    vm: ReaderViewModel = hiltViewModel<ReaderViewModel, ReaderViewModel.Factory> { it.create(contentId) },
) {
    LaunchedEffect(vm.redirect) { vm.redirect?.let(onClose) }
    // The web's focus check and hide(): coming back reads the saved state, leaving sends at once.
    val activity = LocalActivity.current
    LifecycleResumeEffect(vm) {
        vm.setVisible(true)
        onPauseOrDispose { if (activity?.isChangingConfigurations != true) vm.setVisible(false) }
    }
    SyncSnackbars(vm.sync)
    val snackbars = LocalSnackbars.current
    val resources = LocalContext.current
    LaunchedEffect(vm) { vm.messages.collect { snackbars.show(it.string(resources)) } }
    // Opening can ask too: the reader opens where the answer leaves it.
    SyncPromptDialog(vm.sync, vm.title, vm.comic?.content?.type)
    var sheet by rememberSaveable { mutableStateOf(false) }
    val large = isLarge()
    // What the reader has to draw on: Auto spread resolves on it.
    var pageArea by remember { mutableStateOf(IntSize.Zero) }
    val settingsAnchor = remember { PopoverAnchor() }
    val settingsFocus = remember { FocusRequester() }
    val closeSettings = {
        sheet = false
        // The button is in the bars; with them hidden there is nothing to focus.
        runCatching { settingsFocus.requestFocus() }
        Unit
    }
    Surface(Modifier.fillMaxSize()) {
        Row(Modifier.fillMaxSize()) {
            Box(Modifier.weight(1f).fillMaxHeight().onSizeChanged { pageArea = it }, contentAlignment = Alignment.Center) {
                Box(Modifier.safeDrawingPadding().padding(24.dp)) { LoadStatus(vm.comic == null, vm.error, vm::load) }
                val comic = vm.comic ?: return@Box
                // Its files stay for as long as it is composed; what reads them below is keyed by it, so it leaves in the same apply.
                DisposableEffect(comic.pages) {
                    val held = comic.pages.hold()
                    onDispose { held?.close() }
                }
                val content = comic.content
                var chrome by rememberSaveable { mutableStateOf(false) }
                val talkBack = touchExploration()
                val moves = remember { MutableSharedFlow<Boolean>(extraBufferCapacity = 1) }
                val view by vm.sync.view.collectAsState()
                val endCard = @Composable {
                    EndCard(
                        content,
                        vm.series,
                        vm.siblings,
                        vm.readablePrev,
                        vm.readableNext,
                        view,
                        vm.earlierUnread,
                        onVolume = { vm.openVolume(it, onVolume) },
                        onExit = { onClose(content.parentId ?: content.id) },
                        onRetry = vm::retrySiblings,
                        onCompleteSeries = vm.sync::completeSeries,
                    )
                }
                val onPreviousVolume: () -> Unit = { vm.readablePrev?.let { vm.openVolume(it.id, onVolume) } }
                val onNextVolume: () -> Unit = { vm.readableNext?.let { vm.openVolume(it.id, onVolume) } }
                // The page stays usable beside the panel (scrolling, zoom); only a tap on it closes the panel.
                Box(Modifier.fillMaxSize().then(if (sheet && large) Modifier.closeOnTap(closeSettings) else Modifier)) {
                    if (vm.mode == ReaderMode.Longstrip) {
                        LongstripReader(
                            pages = comic.pages,
                            page = vm.page,
                            placement = vm.placement,
                            widthPercent = vm.settings.longstripWidth,
                            hasEndCard = vm.readableNext == null,
                            moves = moves,
                            anchor = vm.stripAnchor,
                            onAnchor = { vm.stripAnchor = it },
                            onPageSettled = vm::onPageSettled,
                            onReachedEnd = vm::onReachedEnd,
                            onPreviousVolume = onPreviousVolume,
                            onNextVolume = onNextVolume,
                            onMenu = { chrome = !chrome },
                            endCard = endCard,
                        )
                    } else {
                        PagedReader(
                            pages = comic.pages,
                            page = vm.page,
                            atEnd = vm.atEnd,
                            placement = vm.placement,
                            settings = vm.settings,
                            shifted = vm.shifted,
                            direction = vm.direction,
                            flipped = vm.controlsFlipped,
                            moves = moves,
                            onPageSettled = vm::onPageSettled,
                            onReachedEnd = vm::onReachedEnd,
                            onPreviousVolume = onPreviousVolume,
                            onNextVolume = onNextVolume,
                            onMenu = { chrome = !chrome },
                            endCard = endCard,
                        )
                    }
                }
                ReaderChrome(
                    // Tap zones can't be explored by touch, so the bars stay.
                    visible = chrome || talkBack,
                    title = readerTitle(content, vm.series, itemLabels()),
                    series = vm.series,
                    page = vm.page,
                    pageCount = comic.pages.pages.size,
                    rtl = vm.mode == ReaderMode.Paged && vm.direction == ReadingDirection.Rtl,
                    siblings = vm.siblings,
                    prev = vm.readablePrev,
                    next = vm.readableNext,
                    pageButtons = talkBack,
                    onBack = { onClose(null) },
                    onSettings = { sheet = true },
                    settingsAnchor = settingsAnchor,
                    settingsFocus = settingsFocus,
                    onMove = { moves.tryEmit(it) },
                    onPlace = vm::placePage,
                    onVolume = onVolume,
                    onAdjacent = { vm.openVolume(it, onVolume) },
                    onDetails = { onClose(content.id) },
                    onRetrySiblings = vm::retrySiblings,
                )
                SaveBanner(view, chrome || talkBack, vm.sync::check, vm.sync::retry, Modifier.align(Alignment.TopCenter))
                if (sheet && !large) ReaderSettingsSheet(vm, pageArea, { sheet = false }, settingsAnchor)
                ReaderTutorial(vm.controlsFlipped)
            }
            VSidePanel(sheet && large && vm.comic != null, stringResource(R.string.reader_display), closeSettings, anchor = settingsAnchor) {
                ReaderSettingsRows(vm, pageArea)
            }
        }
    }
}

/**
 * Closes on a tap: a press and release that stay put. Seen before the page, which then doesn't get
 * the tap; a drag or a pinch goes to the page as always.
 */
@Composable
private fun Modifier.closeOnTap(onTap: () -> Unit): Modifier {
    // Read when the tap ends: a recomposition in between must not restart the gesture.
    val current by rememberUpdatedState(onTap)
    return pointerInput(Unit) { awaitTaps { current() } }
}

private suspend fun PointerInputScope.awaitTaps(onTap: () -> Unit) {
    awaitEachGesture {
        val down = awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Initial)
        while (true) {
            val event = awaitPointerEvent(PointerEventPass.Initial)
            if (event.changes.size > 1 || event.changes.any { (it.position - down.position).getDistance() > viewConfiguration.touchSlop }) return@awaitEachGesture
            val change = event.changes.firstOrNull { it.id == down.id } ?: return@awaitEachGesture
            if (change.changedToUp()) {
                change.consume()
                onTap()
                return@awaitEachGesture
            }
        }
    }
}
