package me.tijlvdb.voltis.ui.content

import android.os.Build
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyGridScope
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.FilterQuality
import androidx.compose.ui.semantics.semantics
import me.tijlvdb.voltis.data.db.DownloadEntity
import me.tijlvdb.voltis.ui.animationsEnabled
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.MutableIntState
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.draw.blur
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.layout
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.ProgressBarRangeInfo
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.paneTitle
import androidx.compose.ui.semantics.progressBarRangeInfo
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.flow.flowOf
import coil3.compose.AsyncImage
import java.util.Locale
import kotlinx.serialization.json.JsonNull
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ContinueReason
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.data.api.UserPrefs
import me.tijlvdb.voltis.data.content.coverUrl
import me.tijlvdb.voltis.domain.catalog.FacetRef
import me.tijlvdb.voltis.domain.catalog.coverProgress
import me.tijlvdb.voltis.domain.catalog.isReadable
import me.tijlvdb.voltis.domain.catalog.isSeries
import me.tijlvdb.voltis.domain.catalog.itemEyebrow
import me.tijlvdb.voltis.domain.catalog.offersCompleting
import me.tijlvdb.voltis.domain.catalog.opens
import me.tijlvdb.voltis.ui.cardLabels
import me.tijlvdb.voltis.ui.downloads.DeleteDownloadDialog
import me.tijlvdb.voltis.ui.downloads.DownloadButton
import me.tijlvdb.voltis.ui.downloads.DownloadButtonViewModel
import me.tijlvdb.voltis.ui.downloads.DownloadSheet
import me.tijlvdb.voltis.ui.downloads.SeriesDownloads
import me.tijlvdb.voltis.ui.downloads.rememberAskNotifications
import me.tijlvdb.voltis.ui.isOnline
import me.tijlvdb.voltis.ui.itemLabels
import me.tijlvdb.voltis.ui.kit.PopoverAnchor
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.popoverAnchor
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VCover
import me.tijlvdb.voltis.ui.nav.HeroKey
import me.tijlvdb.voltis.ui.nav.LocalHeroTarget
import me.tijlvdb.voltis.ui.kit.VDisabled
import me.tijlvdb.voltis.ui.kit.VIconButton
import me.tijlvdb.voltis.ui.kit.VIconButtonStyle
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VProgressBar
import me.tijlvdb.voltis.ui.kit.VTag
import me.tijlvdb.voltis.ui.kit.VTone
import me.tijlvdb.voltis.ui.kit.pageTitleStyle
import me.tijlvdb.voltis.ui.pageGutter
import me.tijlvdb.voltis.ui.rememberCoverRequest
import me.tijlvdb.voltis.ui.theme.VoltisShapes
import me.tijlvdb.voltis.ui.typeLabel

/** The page's own state, above whatever scrolls: an item that has scrolled away must not lose it, and the dialogs it opens outlive it. */
class HeaderState(
    val coverOpen: MutableState<Boolean>,
    val expanded: MutableState<Boolean>,
    /** The series page's tab: [TAB_CONTENTS] or [TAB_DETAILS]. Saved, so it survives a rotation. */
    val tab: MutableState<Int>,
    /** The tab row's height in pixels, as last laid out: the Details body is as tall as the page without it. */
    val tabHeight: MutableIntState,
    val deletingDownload: MutableState<Boolean>,
    val downloadSheet: MutableState<Boolean>,
    /** TalkBack returns to the button when the sheet closes. */
    val downloadAnchor: PopoverAnchor,
    /** Here, not in the sheet: the sheet closes as it asks. */
    val askNotifications: () -> Unit,
)

@Composable
fun rememberHeaderState(): HeaderState {
    val askNotifications = rememberAskNotifications()
    val anchor = remember { PopoverAnchor() }
    val coverOpen = rememberSaveable { mutableStateOf(false) }
    val expanded = rememberSaveable { mutableStateOf(false) }
    val tab = rememberSaveable { mutableStateOf(TAB_CONTENTS) }
    val tabHeight = remember { mutableIntStateOf(0) }
    val deleting = rememberSaveable { mutableStateOf(false) }
    val sheet = rememberSaveable { mutableStateOf(false) }
    return HeaderState(coverOpen, expanded, tab, tabHeight, deleting, sheet, anchor, askNotifications)
}

/** The dialogs the header's buttons open. */
@Composable
fun HeaderDialogs(content: Content, state: HeaderState) {
    if (state.deletingDownload.value) DeleteDownloadDialog(content, onDismiss = { state.deletingDownload.value = false })
    if (state.downloadSheet.value) {
        DownloadSheet(content, state.askNotifications, onDismiss = { state.downloadSheet.value = false }, anchor = state.downloadAnchor)
    }
}

/**
 * The cover's width beside the rest from 600 dp of the page's own width, as a column; null below, where everything is
 * one centred column. [viewport] is what the page was given (after the rail and the insets): the gutters come off it here.
 */
@Composable
fun headerCoverWidth(viewport: Dp): Dp? {
    val width = viewport - pageGutter() * 2
    return when {
        width >= 960.dp -> 240.dp
        width >= 600.dp -> 180.dp
        else -> null
    }
}

const val TAB_CONTENTS = 0
const val TAB_DETAILS = 1

/** How many items [infoHeaderItems] adds before the tabs (or the Contents heading): the first thing after them is the page's next item. */
fun headerItemCount(coverWidth: Dp?) = if (coverWidth != null) 2 else 4

/** The cover beside the title, in the one-column layout. */
private val NarrowCover = 116.dp

/**
 * The top of a content page, the port of `pages/content/InfoHeader.vue`, as grid items: the notices, then
 * the cover with the title, the actions, the status and rating and a short description, each small enough to be composed
 * in a frame, and a part that scrolled away comes back as it was. Then the tabs, Contents and Details, always (the header's
 * geometry never depends on what the server has sent yet); and the details themselves while their tab is open, at least
 * [viewport] tall less the tab row, so switching to them keeps the tab row where it was. The description is in Details.
 * Beside a [coverWidth] wide cover the title and actions are one item (the cover is as tall as it needs to be next to them).
 * What follows (the toolbar and the cards) belongs to the Contents tab.
 */
fun LazyGridScope.infoHeaderItems(
    content: Content,
    vm: ContentViewModel,
    state: HeaderState,
    details: Details,
    coverWidth: Dp?,
    viewport: Dp,
    selectTab: (Int) -> Unit,
    openContent: (String) -> Unit,
    openReader: (String) -> Unit,
    openFacet: (FacetRef) -> Unit,
    openDownloads: (seriesId: String) -> Unit,
    /** Space above the first item: the floating back button sits there. */
    top: Dp,
    notices: @Composable () -> Unit,
) {
    val full: androidx.compose.foundation.lazy.grid.LazyGridItemSpanScope.() -> GridItemSpan = { GridItemSpan(maxLineSpan) }
    item(span = full, contentType = "header-notices") { Box(Modifier.padding(top = top)) { notices() } }
    if (coverWidth != null) {
        item(span = full, contentType = "header-head") {
            WideHead(content, vm, state, coverWidth, false, openContent, openReader, openFacet, openDownloads)
        }
    } else {
        item(span = full, contentType = "header-head") { NarrowHead(content, vm, state, openContent) }
        item(span = full, contentType = "header-actions") {
            Column(Modifier.fillMaxWidth().padding(top = 16.dp)) { NarrowActions(content, vm, state, openContent, openReader) }
        }
        item(span = full, contentType = "header-status") {
            Column(Modifier.fillMaxWidth().padding(top = 8.dp), Arrangement.spacedBy(8.dp)) {
                StatusRow(content, vm)
                StatusExtras(content, vm, wide = true)
                SeriesDownloadsLine(content, vm, openDownloads, Modifier.padding(top = 4.dp))
            }
        }
    }
    item(span = full, contentType = "tabs") {
        ContentTabs(state.tab.value, selectTab, Modifier.padding(top = 12.dp).onSizeChanged { state.tabHeight.intValue = it.height })
    }
    if (state.tab.value == TAB_DETAILS) {
        item(span = full, contentType = "details") {
            val below = viewport - with(LocalDensity.current) { state.tabHeight.intValue.toDp() }
            Box(Modifier.fillMaxWidth().heightIn(min = below)) {
                Box(Modifier.widthIn(max = 640.dp)) { DetailsPanel(content, details, state.expanded, openFacet, Modifier.padding(top = 8.dp)) }
            }
        }
    }
}

/** Contents and Details. The labels are the tabs' names: a selected one is announced as such by the tab role. */
@Composable
private fun ContentTabs(tab: Int, select: (Int) -> Unit, modifier: Modifier = Modifier) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    PrimaryTabRow(tab, modifier, containerColor = Color.Transparent) {
        Tab(tab == TAB_CONTENTS, { select(TAB_CONTENTS) }, unselectedContentColor = muted, text = { Text(stringResource(R.string.content_contents)) })
        Tab(tab == TAB_DETAILS, { select(TAB_DETAILS) }, unselectedContentColor = muted, text = { Text(stringResource(R.string.content_details)) })
    }
}

/** The same sections in one column, for a page that doesn't scroll as a grid: no tabs, the details follow. [coverWidth] as in [infoHeaderItems]. */
@Composable
fun InfoHeader(
    content: Content,
    vm: ContentViewModel,
    state: HeaderState,
    details: Details,
    coverWidth: Dp?,
    openContent: (String) -> Unit,
    openReader: (String) -> Unit,
    openFacet: (FacetRef) -> Unit,
    openDownloads: (seriesId: String) -> Unit,
) {
    if (coverWidth != null) {
        Column {
            WideHead(content, vm, state, coverWidth, true, openContent, openReader, openFacet, openDownloads)
            Box(Modifier.padding(start = coverWidth + 32.dp).widthIn(max = 640.dp)) { DetailRows(details.rows, openFacet, Modifier.padding(top = 20.dp)) }
        }
    } else {
        Column(Modifier.fillMaxWidth()) {
            NarrowHead(content, vm, state, openContent)
            Column(Modifier.fillMaxWidth().padding(top = 16.dp), Arrangement.spacedBy(8.dp)) {
                NarrowActions(content, vm, state, openContent, openReader)
                StatusRow(content, vm)
                StatusExtras(content, vm, wide = true)
            }
            SeriesDownloadsLine(content, vm, openDownloads, Modifier.padding(top = 4.dp))
            DescriptionBlock(content, state.expanded, Modifier.padding(top = 16.dp))
            DetailRows(details.rows, openFacet, Modifier.padding(top = 16.dp))
        }
    }
}

/** The cover on the left; on the right the title, what kind of thing it is, and the progress. */
@Composable
private fun NarrowHead(content: Content, vm: ContentViewModel, state: HeaderState, openContent: (String) -> Unit) {
    Row(Modifier.fillMaxWidth().padding(top = 12.dp), Arrangement.spacedBy(16.dp)) {
        Box(Modifier.width(NarrowCover)) { Cover(content, coverUrl(content), state.coverOpen) }
        Column(Modifier.weight(1f), Arrangement.spacedBy(12.dp)) {
            Heading(content, vm.parent, vm.parentPending, openContent, wide = true, titleStyle = MaterialTheme.typography.headlineSmall)
            CoverProgress(content, vm.next?.earlierUnreadId.takeIf { content.isSeries }, openContent)
        }
    }
}

/** Continue, with the series' download as an icon at its end; a volume's download is under them. */
@Composable
private fun NarrowActions(content: Content, vm: ContentViewModel, state: HeaderState, openContent: (String) -> Unit, openReader: (String) -> Unit) {
    Column(Modifier.fillMaxWidth(), Arrangement.spacedBy(8.dp)) {
        Row(Modifier.fillMaxWidth(), Arrangement.spacedBy(8.dp), Alignment.CenterVertically) {
            ContinuePart(content, vm, openContent, openReader, Modifier.weight(1f), Modifier.fillMaxWidth())
            SeriesDownloadIcon(content, vm, state)
        }
        VolumeDownload(content, state, Modifier.fillMaxWidth())
    }
}

/** Cover, then the title and actions and the description in a column beside it. */
@Composable
private fun WideHead(
    content: Content,
    vm: ContentViewModel,
    state: HeaderState,
    coverWidth: Dp,
    descriptionInHeader: Boolean,
    openContent: (String) -> Unit,
    openReader: (String) -> Unit,
    openFacet: (FacetRef) -> Unit,
    openDownloads: (seriesId: String) -> Unit,
) {
    Row(Modifier.fillMaxWidth().padding(top = 12.dp), Arrangement.spacedBy(32.dp)) {
        CoverColumn(content, vm, state, openContent, Modifier.width(coverWidth))
        Column(Modifier.weight(1f), Arrangement.spacedBy(20.dp)) {
            Heading(content, vm.parent, vm.parentPending, openContent, wide = true)
            // Continue, the status, the star and the menu wrap in a row, as on the web; marking completed and the rating follow.
            Column(Modifier.fillMaxWidth(), Arrangement.spacedBy(8.dp), Alignment.Start) {
                FlowRow(Modifier.fillMaxWidth(), Arrangement.spacedBy(8.dp), Arrangement.spacedBy(8.dp), itemVerticalAlignment = Alignment.CenterVertically) {
                    ContinuePart(content, vm, openContent, openReader, Modifier, Modifier)
                    SeriesDownloadIcon(content, vm, state)
                    VolumeDownload(content, state, Modifier)
                    StatusRow(content, vm, wide = true)
                }
                StatusExtras(content, vm, wide = true)
            }
            SeriesDownloadsLine(content, vm, openDownloads)
            if (descriptionInHeader) Box(Modifier.widthIn(max = 640.dp)) { DescriptionBlock(content, state.expanded, Modifier) }
        }
    }
}

@Composable
private fun CoverColumn(content: Content, vm: ContentViewModel, state: HeaderState, openContent: (String) -> Unit, modifier: Modifier) {
    Column(modifier, Arrangement.spacedBy(12.dp)) {
        Cover(content, coverUrl(content), state.coverOpen)
        CoverProgress(content, vm.next?.earlierUnreadId.takeIf { content.isSeries }, openContent)
    }
}

@Composable
private fun SeriesDownloadsLine(content: Content, vm: ContentViewModel, openDownloads: (String) -> Unit, modifier: Modifier = Modifier) {
    val rows by vm.seriesDownloads.collectAsStateWithLifecycle()
    val auto by vm.autoDownload.collectAsStateWithLifecycle()
    if (rows == null) {
        // Not answered yet: a series that last had rows holds the line's place, a text button at the current font size (48 dp at least). Never seen: nothing is held.
        if (vm.holdsDownloadsLine && content.type == ContentType.COMIC_SERIES) {
            val label = with(LocalDensity.current) { MaterialTheme.typography.labelLarge.lineHeight.toDp() }
            Spacer(modifier.height(maxOf(48.dp, label + 16.dp)))
        }
        return
    }
    SeriesDownloads(content, rows.orEmpty(), auto, openDownloads, modifier)
}

/** Why what needs the server is disabled, from what is stored (P2 §10); null online. */
@Composable
private fun offlineReason(vm: ContentViewModel): String? = if (vm.cached || !isOnline()) stringResource(R.string.error_needs_connection) else null

/**
 * Continue, or Read for a downloaded volume. A book has no reader yet. [box] places it (a weight, say), [button] sizes the
 * button inside it.
 */
@Composable
private fun ContinuePart(content: Content, vm: ContentViewModel, openContent: (String) -> Unit, openReader: (String) -> Unit, box: Modifier, button: Modifier) {
    val offline = offlineReason(vm)
    if (!content.isReadable) {
        Text(
            stringResource(R.string.content_books_unsupported),
            box.padding(vertical = 8.dp),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    } else if (vm.cached && !content.isSeries) {
        // A downloaded volume: the reader opens its copy.
        VButton(stringResource(R.string.content_read), { openReader(content.id) }, box.then(button), icon = VIcons.BookOpen)
    } else {
        // Offline it reads the next volume only from a copy on this device; a failed lookup is Retry, which needs the server.
        val targetId = vm.next?.opens
        val copy by vm.nextCopy.collectAsStateWithLifecycle()
        val notDownloaded = when {
            offline == null || copy -> null
            targetId != null -> stringResource(R.string.content_continue_not_downloaded)
            // Nothing to read: it is as online, a button that explains itself or needs the server.
            vm.next?.reason == ContinueReason.EMPTY -> null
            else -> offline
        }
        VDisabled(notDownloaded, continueText(vm.next, vm.nextFailed, content), box) { enabled ->
            ContinueButton(vm.next, vm.nextFailed, enabled && !vm.busy && vm.ready, vm::retryNext, openReader, openContent, vm::readAgain, button, content)
        }
    }
}

/** A series' download: an icon beside Continue, always shown for a comic series; it opens the volumes to choose from. */
@Composable
private fun SeriesDownloadIcon(content: Content, vm: ContentViewModel, state: HeaderState) {
    if (!content.isReadable || content.type != ContentType.COMIC_SERIES) return
    val name = stringResource(R.string.downloads_download_series)
    VDisabled(offlineReason(vm), name, anchor = state.downloadAnchor) { enabled ->
        VIconButton(
            VIcons.Download,
            name,
            { state.downloadSheet.value = true },
            if (enabled) Modifier.popoverAnchor(state.downloadAnchor) else Modifier,
            VIconButtonStyle.Tonal,
            enabled = enabled,
        )
    }
}

/** A volume's own download, with its progress and its copy. */
@Composable
private fun VolumeDownload(content: Content, state: HeaderState, modifier: Modifier) {
    if (content.isReadable && content.type == ContentType.COMIC) DownloadButton(content, { state.deletingDownload.value = true }, modifier)
}

/** Marking completed, and the rating. */
@Composable
private fun StatusExtras(content: Content, vm: ContentViewModel, wide: Boolean) {
    // No gap: the text button's own touch height already spaces the rating from it.
    Column(Modifier.fillMaxWidth(), horizontalAlignment = if (wide) Alignment.Start else Alignment.CenterHorizontally) {
        if (content.offersCompleting) {
            VButton(
                stringResource(R.string.status_mark_completed),
                { vm.setStatus(ReadingStatus.COMPLETED) },
                Modifier.align(Alignment.Start),
                VButtonStyle.Text,
                enabled = !vm.busy && vm.ready,
            )
        }
        RatingRow(content.userData?.rating, vm::rate, enabled = vm.ready, centred = !wide)
    }
}

/**
 * What a page shows where nothing is known of it yet: the header's shape, so the real one replaces it without
 * moving anything. Not announced as a list of parts: one "Loading".
 */
@Composable
fun HeaderSkeleton(coverWidth: Dp?, modifier: Modifier = Modifier) {
    val fill = MaterialTheme.colorScheme.surfaceContainerHigh
    val loading = stringResource(R.string.loading)
    fun Modifier.bar(width: Dp, height: Dp) = this.size(width, height).background(fill, RoundedCornerShape(8.dp))
    Box(modifier.fillMaxWidth().semantics(mergeDescendants = true) { contentDescription = loading }) {
        if (coverWidth != null) {
            Row(Modifier.fillMaxWidth().padding(top = 12.dp), Arrangement.spacedBy(32.dp)) {
                Box(Modifier.width(coverWidth).aspectRatio(2f / 3f).background(fill, VoltisShapes.cover))
                Column(Modifier.weight(1f), Arrangement.spacedBy(20.dp)) {
                    Box(Modifier.bar(280.dp, 44.dp))
                    Box(Modifier.bar(200.dp, 48.dp))
                    Box(Modifier.bar(320.dp, 24.dp))
                }
            }
        } else {
            Column(Modifier.fillMaxWidth().padding(top = 12.dp)) {
                Row(Modifier.fillMaxWidth(), Arrangement.spacedBy(16.dp)) {
                    Box(Modifier.width(NarrowCover).aspectRatio(2f / 3f).background(fill, VoltisShapes.cover))
                    Column(Modifier.weight(1f), Arrangement.spacedBy(12.dp)) {
                        Box(Modifier.bar(160.dp, 28.dp))
                        Box(Modifier.bar(120.dp, 28.dp))
                    }
                }
                Box(Modifier.padding(top = 16.dp).fillMaxWidth().height(48.dp).background(fill, RoundedCornerShape(24.dp)))
            }
        }
    }
}

/**
 * Where the page has scrolled to, in pixels from the top of the first item: from the rows that are in
 * view, with the heights of the header items above them as they were last seen. Past the header's last
 * item (or when a height was never seen) the backdrop is far enough up to be gone.
 */
fun LazyGridState.headerScroll(heights: IntArray): Float {
    val visible = layoutInfo.visibleItemsInfo
    val first = visible.firstOrNull() ?: return 0f
    for (item in visible) if (item.index < heights.size) heights[item.index] = item.size.height
    if (first.index >= heights.size) return Float.MAX_VALUE
    var top = -first.offset.y.toFloat()
    for (i in 0 until first.index) top += heights[i]
    return top
}

/**
 * Decorative: the cover, blurred, behind the page. It fades in under the top bar, so it has no hard edge
 * there, and out into the page, as the web's mask does. The blur is the image itself: a 24 px thumbnail
 * scaled up with filtering, so nothing is blurred as the page draws. [scroll] is how far the page has
 * scrolled, in pixels.
 */
@Composable
fun HeaderBackdrop(cover: String, scroll: () -> Float, modifier: Modifier = Modifier) {
    val colors = MaterialTheme.colorScheme
    val dark = colors.background.luminance() < 0.5f
    val surface = colors.surface
    Box(
        modifier
            .fillMaxWidth()
            .height(420.dp)
            .graphicsLayer { translationY = -scroll().coerceAtMost(size.height) }
            .clearAndSetSemantics {},
    ) {
        AsyncImage(
            rememberCoverRequest(cover, crossfade = animationsEnabled(), backdrop = true),
            contentDescription = null,
            Modifier.matchParentSize(),
            contentScale = ContentScale.Crop,
            alpha = if (dark) 0.16f else 0.3f,
            filterQuality = FilterQuality.Medium,
        )
        Box(
            Modifier.matchParentSize().drawBehind {
                val fadeIn = 48.dp.toPx() / size.height
                drawRect(Brush.verticalGradient(0f to surface, fadeIn to surface.copy(alpha = 0f), 1f to surface))
            },
        )
    }
}

/** The cover; a tap shows it full screen. */
@Composable
private fun Cover(content: Content, url: String?, openState: MutableState<Boolean>) {
    var open by openState
    val view = stringResource(R.string.content_view_cover, content.title)
    val click = if (url == null) {
        Modifier
    } else {
        Modifier.clip(VoltisShapes.cover).clickable(role = Role.Button) { open = true }.semantics { contentDescription = view }
    }
    val target = LocalHeroTarget.current
    VCover(url, click, target?.let { HeroKey(content.id, it) })
    if (open && url != null) {
        val title = stringResource(R.string.content_cover, content.title)
        val close = stringResource(R.string.content_close_cover)
        Dialog({ open = false }, DialogProperties(usePlatformDefaultWidth = false)) {
            Box(
                Modifier
                    .fillMaxSize()
                    .semantics { paneTitle = title }
                    .clickable(interactionSource = null, indication = null, onClickLabel = close) { open = false }
                    .padding(16.dp),
                Alignment.Center,
            ) {
                AsyncImage(rememberCoverRequest(url, sized = false), contentDescription = title, Modifier.fillMaxWidth().clip(VoltisShapes.cover), contentScale = ContentScale.Fit)
            }
        }
    }
}

/** Under the cover: the progress, what is new in a completed series, and the way to earlier unread volumes. */
@Composable
private fun CoverProgress(content: Content, earlierId: String?, openContent: (String) -> Unit) {
    val labels = cardLabels()
    val progress = remember(content, labels) { coverProgress(content, labels.progress, labels.caughtUp) }
    val added = (content.newChildrenCount ?: 0).takeIf { content.userData?.status == ReadingStatus.COMPLETED && it > 0 }
    if (progress == null && added == null && earlierId == null) return
    val colors = MaterialTheme.colorScheme
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        if (progress != null || added != null) {
            // A long label ("150/150 read · Caught up") takes a line of its own.
            FlowRow(Modifier.fillMaxWidth(), Arrangement.SpaceBetween, itemVerticalAlignment = Alignment.CenterVertically) {
                // The bar says both.
                Text(if (progress != null) stringResource(R.string.content_progress) else "", Modifier.clearAndSetSemantics {}, style = MaterialTheme.typography.bodySmall)
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                    if (added != null) VTag(labels.newCount.format(added), tone = VTone.Primary)
                    if (progress != null) {
                        Text(progress.label, Modifier.clearAndSetSemantics {}, color = colors.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        }
        if (progress != null) {
            val name = stringResource(R.string.reader_progress)
            VProgressBar(
                progress.fraction,
                Modifier.fillMaxWidth().semantics {
                    contentDescription = name
                    stateDescription = progress.label
                    progressBarRangeInfo = ProgressBarRangeInfo(progress.fraction, 0f..1f)
                },
            )
        }
        if (earlierId != null) {
            // Only comic series ask what comes next: their children are chapters.
            VButton(
                stringResource(R.string.content_earlier_chapters),
                { openContent(earlierId) },
                style = VButtonStyle.Text,
            )
        }
    }
}

/** The way up to the series, the item's number, the title and what kind of thing it is. */
@Composable
private fun Heading(
    content: Content,
    parent: Content?,
    parentPending: Boolean,
    openContent: (String) -> Unit,
    wide: Boolean,
    titleStyle: TextStyle? = null,
) {
    val colors = MaterialTheme.colorScheme
    val align = if (wide) Alignment.Start else Alignment.CenterHorizontally
    val labels = itemLabels()
    Column(horizontalAlignment = align, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        // Until the series' row arrives its slots are held at their real size (the button's 48 dp, and its font scaling), so nothing moves.
        val pending = parent == null && parentPending
        if (parent != null || pending) {
            // One line, ellipsized: its height is the same whatever the series is called.
            VButton(
                parent?.title ?: "\u00A0",
                { parent?.let { openContent(it.id) } },
                // The series' name lines up with the title: the button's own padding is 12 dp.
                Modifier.offset(x = if (wide) (-12).dp else 0.dp).then(if (pending) Modifier.alpha(0f).clearAndSetSemantics {} else Modifier),
                style = VButtonStyle.Text,
                icon = VIcons.ChevronLeft,
                singleLine = true,
            )
        }
        val eyebrow = remember(content, parent, labels) { itemEyebrow(content, parent, labels) }
        // Whether a numbered item has its number line depends on its series' name: the slot is held, empty if need be, so nothing moves when the series arrives.
        if (eyebrow != null || (content.parentId != null && content.orderParts.isNotEmpty())) {
            Text(eyebrow ?: "\u00A0", color = colors.onSurfaceVariant, style = MaterialTheme.typography.titleSmall)
        }
        Text(
            content.title,
            Modifier.semantics { heading() },
            style = titleStyle ?: if (wide) pageTitleStyle() else MaterialTheme.typography.displaySmall,
            textAlign = if (wide) TextAlign.Start else TextAlign.Center,
        )
        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp, align), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            val meta = content.meta
            VTag(typeLabel(content.type))
            meta.kind?.takeIf { it.isNotEmpty() }?.let { VTag(it.capitalized()) }
            meta.status?.takeIf { it.isNotEmpty() }?.let { VTag(stringResource(R.string.content_publication, it.capitalized())) }
            meta.language?.takeIf { it.isNotEmpty() }?.let { code ->
                VTag(Locale.forLanguageTag(code).displayName.ifEmpty { code })
            }
        }
    }
}

private fun String.capitalized() = replaceFirstChar { it.uppercase() }
