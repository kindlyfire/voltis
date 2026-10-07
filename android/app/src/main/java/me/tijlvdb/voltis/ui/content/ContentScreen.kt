package me.tijlvdb.voltis.ui.content

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.tween
import androidx.compose.runtime.derivedStateOf
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.input.nestedscroll.NestedScrollConnection
import androidx.compose.ui.input.nestedscroll.NestedScrollSource
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.semantics.isTraversalGroup
import androidx.compose.ui.semantics.traversalIndex
import androidx.compose.ui.unit.Dp
import me.tijlvdb.voltis.ui.animationsEnabled
import me.tijlvdb.voltis.ui.rememberTouchExploration
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.zIndex
import me.tijlvdb.voltis.ui.kit.VIcons
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.content.coverUrl
import me.tijlvdb.voltis.domain.catalog.FacetRef
import me.tijlvdb.voltis.domain.catalog.isSeries
import me.tijlvdb.voltis.ui.EffectHost
import me.tijlvdb.voltis.ui.EndReviewOnStop
import me.tijlvdb.voltis.ui.LoadStatus
import me.tijlvdb.voltis.ui.OfflineBar
import me.tijlvdb.voltis.ui.ServerOffline
import me.tijlvdb.voltis.ui.needsConnection
import me.tijlvdb.voltis.ui.RefreshOnResume
import me.tijlvdb.voltis.ui.bottomSpace
import me.tijlvdb.voltis.ui.grid.ContentGrid
import me.tijlvdb.voltis.ui.grid.GridItems
import me.tijlvdb.voltis.ui.grid.GridSource
import me.tijlvdb.voltis.ui.grid.BulkSelectionHost
import me.tijlvdb.voltis.ui.grid.GridViewModel
import me.tijlvdb.voltis.ui.grid.SelectionTopBar
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.QueryError
import me.tijlvdb.voltis.ui.kit.VNotice
import me.tijlvdb.voltis.ui.lists.LISTS_SHEET
import me.tijlvdb.voltis.ui.lists.ListsSheetViewModel
import me.tijlvdb.voltis.ui.lists.SheetEffect
import me.tijlvdb.voltis.ui.pageGutter
import me.tijlvdb.voltis.ui.reader.SyncPromptDialog

/** The page of a series, a volume or a book: the port of `pages/content/ContentPage.vue`. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ContentScreen(
    id: String,
    onBack: () -> Unit,
    openContent: (contentId: String) -> Unit,
    openReader: (contentId: String) -> Unit,
    openFacet: (FacetRef) -> Unit,
    openDownloads: (seriesId: String) -> Unit,
    vm: ContentViewModel = hiltViewModel<ContentViewModel, ContentViewModel.Factory> { it.create(id) },
) {
    RefreshOnResume(vm.refresher)
    val snackbars = LocalSnackbars.current
    val context = LocalContext.current
    LaunchedEffect(vm) {
        vm.effects.collect { effect ->
            when (effect) {
                is ContentEffect.Message -> snackbars.show(effect.text.string(context))
                is ContentEffect.Read -> openReader(effect.contentId)
            }
        }
    }
    val content = vm.content
    // Kept once the series is known, above the layout switch: going offline keeps the selection's Back and dialogs.
    val contents = if (content?.isSeries == true) {
        hiltViewModel<GridViewModel, GridViewModel.Factory>(key = "contents:$id") { it.create(GridSource.Contents(id)) }
    } else {
        null
    }
    if (contents != null) BulkSelectionHost(contents, hostSheetEffects = false)
    // The add-to-list sheet's writes outlive it: their messages are shown here, in either layout.
    val sheet = hiltViewModel<ListsSheetViewModel>(key = LISTS_SHEET)
    EffectHost(sheet.effects) { effect ->
        when (effect) {
            is SheetEffect.Message -> snackbars.show(effect.text.string(context))
            is SheetEffect.Added -> {
                snackbars.show(effect.text.string(context))
                contents?.listsAdded(effect.opening)
            }
        }
    }
    val headerState = rememberHeaderState()
    Surface(Modifier.fillMaxSize()) {
      // What the page is given, after the rail and the insets: the header's layout follows it.
      BoxWithConstraints {
        val coverWidth = headerCoverWidth(maxWidth)
        val selecting = contents?.selection?.active == true
        // The head starts below the floating back button; a selection's top bar takes its place.
        // The top safe-drawing inset (status bar, cutout): the scrolling page stays below it, the backdrop doesn't.
        val inset = WindowInsets.safeDrawing.only(WindowInsetsSides.Top).asPaddingValues().calculateTopPadding()
        val top = inset + BACK_SPACE
        // Hidden while scrolling down, back on scrolling up and at the top.
        val backShown = remember { mutableStateOf(true) }
        val hideOnScroll = remember {
            object : NestedScrollConnection {
                // What the page did scroll, not what was asked of it: a page that can't scroll never hides it.
                override fun onPostScroll(consumed: Offset, available: Offset, source: NestedScrollSource): Offset {
                    if (consumed.y < -1f) backShown.value = false else if (consumed.y > 1f) backShown.value = true
                    return Offset.Zero
                }
            }
        }
        // Over the page in z; its place in TalkBack's order is its traversal index.
        // Only the scrolling page hides it; the offline, error and loading pages always show it.
        val scrolling = !vm.unavailable && content != null
        LaunchedEffect(scrolling) { backShown.value = true }
        if (!selecting) FloatingBack(onBack, backShown.value || !scrolling, inset, Modifier.align(Alignment.TopStart).zIndex(1f))
        Column(Modifier.fillMaxSize()) {
            val notices: @Composable () -> Unit = {
                Column {
                    if (vm.cached) OfflineBar(Modifier.padding(bottom = 12.dp), shown = true, onRetry = vm::refresh)
                    QueryError(vm.error, Modifier.padding(bottom = 12.dp), vm::refresh)
                    if (vm.needsReview) {
                        VNotice(
                            stringResource(R.string.sync_changed_elsewhere),
                            stringResource(R.string.sync_review),
                            vm::review,
                            Modifier.padding(bottom = 12.dp),
                            reason = needsConnection(),
                            live = vm.reviewAppeared,
                        )
                    }
                }
            }
            val cover = content?.let(::coverUrl)
            when {
                vm.unavailable -> {
                    if (contents != null && selecting) SelectionTopBar(contents.selection.items.size) { contents.selecting(false) } else Spacer(Modifier.height(top))
                    ServerOffline(onRetry = vm::refresh)
                }
                content == null -> {
                    Spacer(Modifier.height(top))
                    if (vm.error != null) {
                        Box(Modifier.fillMaxSize().padding(horizontal = pageGutter(), vertical = 16.dp), Alignment.TopCenter) { LoadStatus(true, vm.error, vm::refresh) }
                    } else {
                        // Nothing is known of the page yet: the header's shape, where it will be.
                        HeaderSkeleton(coverWidth, Modifier.padding(horizontal = pageGutter()))
                    }
                }
                contents != null -> {
                    // The page is one grid: the header's items and the heading are its first.
                    RefreshOnResume(contents.refresher)
                    val grid = rememberLazyGridState()
                    val headerItems = headerItemCount(coverWidth)
                    val details = rememberDetails(content, vm)
                    if (contents.selection.active) SelectionTopBar(contents.selection.items.size) { contents.selecting(false) }
                    val atTop by remember { derivedStateOf { grid.firstVisibleItemIndex == 0 && grid.firstVisibleItemScrollOffset == 0 } }
                    LaunchedEffect(atTop) { if (atTop) backShown.value = true }
                    val pad = if (contents.selection.active) 0.dp else inset
                    // Clipped: the backdrop moves with the scroll.
                    BoxWithConstraints(Modifier.weight(1f).fillMaxWidth().clipToBounds().nestedScroll(hideOnScroll)) {
                        // What the foreground is given, below the inset: Details is at least this tall less the tab row.
                        val viewport = maxHeight - pad
                        // The tabs leave the scroll where it is: Details is at least as tall as the page, so the tab row stays put.
                        val selectTab: (Int) -> Unit = { tab -> headerState.tab.value = tab }
                        if (cover != null) {
                            val heights = remember(headerItems) { IntArray(headerItems) }
                            HeaderBackdrop(cover, { grid.headerScroll(heights) })
                        }
                        Box(Modifier.fillMaxSize().padding(top = pad)) {
                        ContentGrid(
                            contents,
                            GridItems(contents.list, vm.refreshing || contents.listRefreshing, contents.listError, contents::refresh),
                            openContent,
                            openReader,
                            state = grid,
                            parent = content,
                            onRefresh = {
                                vm.refresh()
                                contents.refresh()
                            },
                            // The "Contents" heading: a filter change doesn't jump to the page's top.
                            filterTop = headerItems,
                            // The Details tab replaces the volumes.
                            showItems = headerState.tab.value != TAB_DETAILS,
                        ) {
                            infoHeaderItems(
                                content, vm, headerState, details, coverWidth, viewport, selectTab, openContent, openReader, openFacet, openDownloads,
                                top = if (contents.selection.active) 0.dp else BACK_SPACE, notices = notices,
                            )
                        }
                        }
                    }
                }
                else -> {
                    val scroll = rememberScrollState()
                    val details = rememberDetails(content, vm)
                    val atTop by remember { derivedStateOf { scroll.value == 0 } }
                    LaunchedEffect(atTop) { if (atTop) backShown.value = true }
                    // Clipped: the backdrop moves with the scroll.
                    Box(Modifier.weight(1f).fillMaxWidth().clipToBounds().nestedScroll(hideOnScroll)) {
                        if (cover != null) HeaderBackdrop(cover, { scroll.value.toFloat() })
                        PullToRefreshBox(vm.refreshing, vm::refresh, Modifier.padding(top = inset)) {
                            Column(Modifier.fillMaxSize().verticalScroll(scroll).padding(horizontal = pageGutter()).padding(top = BACK_SPACE, bottom = bottomSpace())) {
                                notices()
                                InfoHeader(
                                    content, vm, headerState, details, coverWidth, openContent, openReader, openFacet, openDownloads,
                                )
                            }
                        }
                    }
                }
            }
        }
      }
    }
    if (content != null) HeaderDialogs(content, headerState)
    if (content != null) ContentDialogs(content, vm)
    EndReviewOnStop(vm.reviewer)
    vm.reviewer.current?.let { SyncPromptDialog(it.session, it.title, content?.type) }
}

/** The floating back button's height plus the gap under it: where the page's head starts below the status bar. */
private val BACK_SPACE = 52.dp

/**
 * A 40 dp circle on a scrim over the backdrop, in a 48 dp target, [inset] below the window's top. Hidden, it fades out and
 * takes no taps, so it never intercepts what is scrolled beneath it. Under TalkBack it stays: the cursor can't land on a node
 * that is not there. Its negative traversal index puts it first, ahead of the scrolling page.
 */
@Composable
private fun FloatingBack(onBack: () -> Unit, shown: Boolean, inset: Dp, modifier: Modifier = Modifier) {
    val gutter = pageGutter()
    val explore by rememberTouchExploration()
    val visible = shown || explore
    val alpha by animateFloatAsState(if (visible) 1f else 0f, if (animationsEnabled()) tween(150) else snap(), label = "back")
    if (alpha == 0f && !visible) return
    Box(
        modifier
            .padding(top = inset)
            .padding(start = (gutter - 4.dp).coerceAtLeast(0.dp))
            .graphicsLayer { this.alpha = alpha }
            .size(48.dp)
            .semantics {
                isTraversalGroup = true
                traversalIndex = -1f
            }
            .clip(CircleShape)
            .then(if (visible) Modifier.clickable(role = Role.Button, onClick = onBack) else Modifier),
        Alignment.Center,
    ) {
        Box(Modifier.size(40.dp).background(MaterialTheme.colorScheme.surface.copy(alpha = 0.72f), CircleShape), Alignment.Center) {
            Icon(VIcons.ArrowLeft, stringResource(R.string.back), Modifier.size(24.dp), tint = MaterialTheme.colorScheme.onSurface)
        }
    }
}
