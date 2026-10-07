package me.tijlvdb.voltis.ui.nav

import android.content.res.Resources
import androidx.activity.ComponentActivity
import androidx.activity.compose.LocalActivity
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.systemBars
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material3.Badge
import androidx.compose.material3.BadgedBox
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.NavigationRailItemDefaults
import androidx.compose.material3.Snackbar
import androidx.compose.material3.SnackbarDuration
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.SnackbarVisuals
import androidx.compose.material3.Text
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteDefaults
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteScaffold
import androidx.compose.material3.adaptive.navigationsuite.NavigationSuiteType
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.layout.boundsInWindow
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.isTraversalGroup
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.traversalIndex
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import androidx.navigation3.runtime.NavEntry
import androidx.navigation3.scene.Scene
import androidx.navigation3.scene.SceneStrategy
import dagger.hilt.android.EntryPointAccessors
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.reading.BulkSummary
import me.tijlvdb.voltis.domain.catalog.BulkWhat
import me.tijlvdb.voltis.domain.layout.NavKind
import me.tijlvdb.voltis.domain.layout.navKind
import me.tijlvdb.voltis.ui.LocalBottomInset
import me.tijlvdb.voltis.ui.LocalSnackbarLift
import me.tijlvdb.voltis.ui.LocalOffline
import me.tijlvdb.voltis.ui.Offline
import me.tijlvdb.voltis.ui.UiText
import me.tijlvdb.voltis.ui.noticeText
import me.tijlvdb.voltis.ui.statusRes
import me.tijlvdb.voltis.ui.kit.LocalSnackbars
import me.tijlvdb.voltis.ui.kit.Snackbars
import me.tijlvdb.voltis.ui.kit.SnackbarsEntryPoint
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.theme.VoltisShapes
import me.tijlvdb.voltis.ui.windowWidthDp

/**
 * The signed-in app's frame: the four tabs around [content] (the NavDisplay), and the snackbars.
 * The tabs are a bottom bar on a narrow window and a rail on a wide one.
 */
@Composable
fun MainScaffold(nav: MainNavState, account: String, content: @Composable () -> Unit) {
    val colors = MaterialTheme.colorScheme
    val scope = rememberCoroutineScope()
    val activity = LocalActivity.current as ComponentActivity
    val model = remember(activity) { EntryPointAccessors.fromActivity(activity, SnackbarsEntryPoint::class.java).snackbars() }
    val snackbars = remember(model) { Snackbars(model, scope) }
    DisposableEffect(model) { onDispose { if (!activity.isChangingConfigurations) model.drop() } }
    val sync = hiltViewModel<ScaffoldViewModel>(activity)
    val attention by sync.attention.collectAsStateWithLifecycle(0)
    val context = LocalContext.current
    val resources = LocalResources.current
    val lifecycle = LocalLifecycleOwner.current
    // A notice nobody was there to see is stored; with the app in front it is also said at once, on whatever screen shows.
    LaunchedEffect(sync) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            sync.fresh.collect { batch ->
                val notice = batch.singleOrNull()
                if (notice != null) {
                    snackbars.show(UiText.Format(R.string.notice_titled, listOf(notice.title, noticeText(notice.kind, notice.detail))).string(context))
                } else {
                    val text = resources.getQuantityString(R.plurals.notices_not_applied, batch.size, batch.size)
                    // Not over an Undo: it waits for that one instead.
                    snackbars.show(text, resources.getString(R.string.notices_view)) { nav.closeReader(AttentionDest) }
                }
            }
        }
    }
    // Queued in the runner until presented, then acknowledged: a rotation or stop cancels a snackbar not yet acknowledged, and it is shown again.
    val summary by sync.summary.collectAsStateWithLifecycle()
    LaunchedEffect(summary?.id) {
        val item = summary ?: return@LaunchedEffect
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            if (!sync.pending(item)) return@repeatOnLifecycle
            val view = resources.getString(R.string.notices_view).takeIf { item.failed > 0 }
            val visuals = SummaryVisuals(item, bulkText(resources, item), view, if (view != null) SnackbarDuration.Long else SnackbarDuration.Short)
            // Not acknowledged when it stopped being valid first: teardown drops it.
            val result = snackbars.state.presentWhile(visuals, sync.valid(item)) ?: return@repeatOnLifecycle
            // Taken down by the host check below: not acknowledged.
            if (!sync.pending(item)) return@repeatOnLifecycle
            sync.presented(item)
            if (result == SnackbarResult.ActionPerformed) nav.closeReader(AttentionDest)
        }
    }
    LaunchedEffect(sync) {
        sync.storageFull.collect { snackbars.show(resources.getString(R.string.error_storage_full)) }
    }
    val itemColors = NavigationSuiteDefaults.itemColors(
        navigationBarItemColors = NavigationBarItemDefaults.colors(
            selectedIconColor = colors.onSecondaryContainer,
            selectedTextColor = colors.onSurface,
            indicatorColor = colors.secondaryContainer,
            unselectedIconColor = colors.onSurfaceVariant,
            unselectedTextColor = colors.onSurfaceVariant,
        ),
        navigationRailItemColors = NavigationRailItemDefaults.colors(
            selectedIconColor = colors.onSecondaryContainer,
            selectedTextColor = colors.onSurface,
            indicatorColor = colors.secondaryContainer,
            unselectedIconColor = colors.onSurfaceVariant,
            unselectedTextColor = colors.onSurfaceVariant,
        ),
    )
    val kind = navKind(nav.reader != null, windowWidthDp())
    val barLabel = barLabelSize(Tab.entries.map { stringResource(it.label) })
    val attentionText = if (attention > 0) pluralStringResource(R.plurals.attention_items, attention, attention) else null
    NavigationSuiteScaffold(
        navigationSuiteItems = {
            for (tab in Tab.entries) {
                val selected = tab == nav.current
                // Material clears the icon slot's semantics: the count is the item's state.
                val badge = attentionText.takeIf { tab == Tab.Libraries }
                item(
                    modifier = Modifier.semantics { badge?.let { stateDescription = it } },
                    selected = selected,
                    onClick = { nav.select(tab) },
                    icon = {
                        if (badge != null) {
                            BadgedBox({ Badge() }) { Icon(tab.icon(selected), contentDescription = null) }
                        } else {
                            Icon(tab.icon(selected), contentDescription = null)
                        }
                    },
                    label = {
                        if (kind == NavKind.Rail) {
                            // A label wider than the rail widens it: the padding keeps it off the rail's edges at large font sizes.
                            Text(stringResource(tab.label), Modifier.padding(horizontal = 8.dp))
                        } else {
                            // A bar item can't widen: the labels shrink to fit one line rather than break inside a word.
                            Text(stringResource(tab.label), fontSize = barLabel, maxLines = 1, softWrap = false, overflow = TextOverflow.Ellipsis)
                        }
                    },
                    colors = itemColors,
                )
            }
        },
        layoutType = when (kind) {
            NavKind.None -> NavigationSuiteType.None
            NavKind.Bar -> NavigationSuiteType.NavigationBar
            NavKind.Rail -> NavigationSuiteType.NavigationRail
        },
        navigationSuiteColors = NavigationSuiteDefaults.colors(
            navigationBarContainerColor = colors.surfaceContainerLow,
            navigationRailContainerColor = colors.surfaceContainerLow,
        ),
        containerColor = colors.background,
    ) {
        // The bar clears the system's navigation bar; beside the rail, content reaches the window's bottom.
        val bottomInset = if (kind == NavKind.Rail) WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding() else 0.dp
        val lift = remember { mutableStateOf(0.dp) }
        val offline = remember(sync, nav) { Offline(sync.online, sync::probe, sync::serverUrl) { nav.showInLibraries(DownloadsDest()) } }
        CompositionLocalProvider(LocalSnackbars provides snackbars, LocalBottomInset provides bottomInset, LocalSnackbarLift provides lift, LocalOffline provides offline) {
            // What the bar covers below the content, so that the keyboard's padding (Search) counts from the bar.
            val windowHeight = LocalWindowInfo.current.containerSize.height
            val density = LocalDensity.current
            var covered by remember { mutableStateOf(0.dp) }
            Box(
                Modifier
                    // Content first: when a window opens or closes TalkBack starts at the content, not the first tab.
                    .semantics { isTraversalGroup = true; traversalIndex = -1f }
                    .onGloballyPositioned { covered = with(density) { (windowHeight - it.boundsInWindow().bottom).coerceAtLeast(0f).toDp() } }
                    .consumeWindowInsets(WindowInsets(bottom = covered)),
            ) {
                content()
                SnackbarHost(
                    snackbars.state,
                    Modifier.align(Alignment.BottomCenter)
                        .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal + WindowInsetsSides.Bottom))
                        .padding(bottom = lift.value),
                ) { data ->
                    // Revalidated when a summary gets the host, and on every composition with another account.
                    val v = data.visuals
                    if (v is SummaryVisuals && (v.item.account != account || !sync.pending(v.item))) {
                        // Renders nothing, so it is never announced.
                        LaunchedEffect(data) { data.dismiss() }
                    } else {
                        Snackbar(data, shape = VoltisShapes.toast)
                    }
                }
            }
        }
    }
}

/** A summary's snackbar: the host shows it only while it is valid for the account rendered. */
private class SummaryVisuals(
    val item: BulkSummary,
    override val message: String,
    override val actionLabel: String?,
    override val duration: SnackbarDuration,
) : SnackbarVisuals {
    override val withDismissAction = false
}

/** Shows [visuals] while [valid] stays true. Null when it stopped being valid first (never shown, or taken down). */
internal suspend fun SnackbarHostState.presentWhile(visuals: SnackbarVisuals, valid: Flow<Boolean>): SnackbarResult? = coroutineScope {
    // Undispatched, so it is queued in arrival order with the others.
    val shown = async(start = CoroutineStart.UNDISPATCHED) { showSnackbar(visuals) }
    val lost = launch {
        valid.first { !it }
        shown.cancel()
    }
    try {
        shown.await()
    } catch (e: CancellationException) {
        // Our own cancel gives null; the caller's propagates.
        ensureActive()
        null
    } finally {
        lost.cancel()
    }
}

/** The web's toasts for a batch, with the failures counted, and the commands a full disk kept unsaved. */
private fun bulkText(resources: Resources, summary: BulkSummary): String {
    val n = summary.done
    val what = summary.what
    val notSaved = summary.notSaved.takeIf { it > 0 }?.let { resources.getQuantityString(R.plurals.bulk_not_saved, it, it) }
    if (n == 0 && summary.failed == 0 && notSaved != null) return notSaved
    val text = if (n == 0) {
        resources.getQuantityString(R.plurals.bulk_all_failed, summary.failed, summary.failed)
    } else {
        val done = when {
            what is BulkWhat.Status && what.status != null ->
                resources.getQuantityString(R.plurals.bulk_set, n, n, statusRes(what.status)?.let { resources.getString(it) } ?: what.status)
            what is BulkWhat.Status -> resources.getQuantityString(R.plurals.bulk_status_cleared, n, n)
            else -> resources.getQuantityString(R.plurals.bulk_cleared, n, n)
        }
        if (summary.failed == 0) done else resources.getQuantityString(R.plurals.bulk_failed, summary.failed, done, summary.failed)
    }
    return if (notSaved == null) text else "$text. $notSaved"
}

/**
 * One size for the bar's labels: the label style's, or smaller until the longest fits its item, but
 * not below [MinTabLabel] (then it ends in an ellipsis). At the default font scale every label fits.
 */
@Composable
private fun barLabelSize(labels: List<String>): TextUnit {
    val style = MaterialTheme.typography.labelMedium
    val measurer = rememberTextMeasurer()
    val density = LocalDensity.current
    val direction = LocalLayoutDirection.current
    val insets = WindowInsets.systemBars
    val window = LocalWindowInfo.current.containerSize.width - insets.getLeft(density, direction) - insets.getRight(density, direction)
    // The bar spaces its items 8 dp apart; the slack keeps a fitted label off the item's edges.
    val item = with(density) { (window - (8.dp * (labels.size - 1)).toPx()) / labels.size - LabelSlack.toPx() }
    val longest = remember(labels, style, density, measurer) { labels.maxOf { measurer.measure(it, style, maxLines = 1, softWrap = false).size.width } }
    return with(density) { maxOf(style.fontSize.toPx() * minOf(1f, item / longest), MinTabLabel.toPx()).toSp() }
}

private val LabelSlack = 8.dp

/** The smallest a bar's tab label shrinks to: about 0.8 of the label style. */
private val MinTabLabel = 10.sp

private val Tab.label
    get() = when (this) {
        Tab.Home -> R.string.tab_home
        Tab.Libraries -> R.string.tab_libraries
        Tab.Search -> R.string.tab_search
        Tab.Settings -> R.string.tab_settings
    }

/** The web's filled icon when selected. */
@Composable
private fun Tab.icon(selected: Boolean) = when (this) {
    Tab.Home -> if (selected) VIcons.HomeFilled else VIcons.Home
    Tab.Libraries -> if (selected) VIcons.BookshelfFilled else VIcons.Bookshelf
    Tab.Search -> VIcons.Magnify
    Tab.Settings -> if (selected) VIcons.CogFilled else VIcons.Cog
}

/** Marks the Home root's NavEntry, which [HomeRootSceneStrategy] looks for. */
val HomeRootMetadata = mapOf<String, Any>(HOME_ROOT to true)

private const val HOME_ROOT = "homeRoot"

/**
 * NavDisplay handles Back whenever its scene has entries behind it, and its list always holds
 * the four roots. With the Home root on top (the Home tab, nothing pushed, no reader) there is
 * nothing to go back to inside the app, so that scene declares nothing behind it: Back is then
 * the system's, which shows its own back-to-home animation and keeps the task.
 */
val HomeRootSceneStrategy = SceneStrategy<Entry> { entries ->
    entries.last().takeIf { HOME_ROOT in it.metadata }?.let(::HomeRootScene)
}

private data class HomeRootScene(val entry: NavEntry<Entry>) : Scene<Entry> {
    override val key: Any = entry.contentKey
    override val entries = listOf(entry)
    override val previousEntries = emptyList<NavEntry<Entry>>()
    override val content: @Composable () -> Unit = { entry.Content() }
}
