package me.tijlvdb.voltis.ui.grid

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.dropShadow
import androidx.compose.ui.graphics.painter.Painter
import androidx.compose.ui.graphics.shadow.Shadow
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.em
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.data.content.coverUrl
import me.tijlvdb.voltis.domain.catalog.GridOptions
import me.tijlvdb.voltis.domain.catalog.cardText
import me.tijlvdb.voltis.domain.catalog.contentProgress
import me.tijlvdb.voltis.domain.downloads.DownloadBadge
import me.tijlvdb.voltis.ui.cardLabels
import me.tijlvdb.voltis.ui.downloads.DownloadBadgeIcon
import me.tijlvdb.voltis.ui.downloads.downloadBadgeLabel
import me.tijlvdb.voltis.ui.kit.VBadge
import me.tijlvdb.voltis.ui.kit.VCover
import me.tijlvdb.voltis.ui.nav.HeroKey
import me.tijlvdb.voltis.ui.nav.LocalHeroOrigin
import me.tijlvdb.voltis.ui.nav.LocalHeroTaps
import me.tijlvdb.voltis.ui.kit.VIconButton
import me.tijlvdb.voltis.ui.kit.VIconButtonStyle
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.kit.VProgressBar
import me.tijlvdb.voltis.ui.theme.VoltisShapes
import me.tijlvdb.voltis.ui.theme.VoltisTheme

/**
 * A cover with its caption: the port of `ContentGrid/Item.vue`.
 *
 * A tap calls [onOpen] for the content's page. With [onRead] it is a "read" card: a tap opens the
 * reader, and a details button opens the page of [series], or of the item without one. A book is
 * always a plain card, since books can't be read yet.
 *
 * [series] shows [content] as the item to read next in it; [parent] shows it as an item of that
 * series, by its number and shortened title. [isNew] marks an item added since the
 * user caught up with its series. [highlightReading] glows the cover of an item being read, which
 * only grids do. [download] is the item's download mark, at the cover's bottom left. With
 * [fitWidth], the card's width, a subtitle that doesn't fit it names the item "Vol. 1" rather than
 * "Volume 1" (a narrow card at a large font); TalkBack still reads the full words. [subtitle] is
 * shown when the card has none of its own (Search's content type).
 *
 * With [onToggle] the card can be selected: a long press (or the "Select" action) calls it, and in
 * [selecting] mode a tap does, instead of opening anything. The card is then a checkbox, [selected]
 * or not, with a check circle on its cover. With [unavailable] (offline and not downloaded) the card
 * is dimmed and says why.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun ContentCard(
    content: Content,
    onOpen: (contentId: String) -> Unit,
    modifier: Modifier = Modifier,
    series: Content? = null,
    isNew: Boolean = false,
    onRead: ((contentId: String) -> Unit)? = null,
    options: GridOptions = GridOptions(),
    highlightReading: Boolean = false,
    parent: Content? = null,
    download: DownloadBadge? = null,
    fitWidth: Dp? = null,
    subtitle: String? = null,
    selecting: Boolean = false,
    selected: Boolean = false,
    onToggle: (() -> Unit)? = null,
    unavailable: String? = null,
    /** Tells apart the rows of one screen that may show the same item (Home's), for the hero transition. */
    origin: String = "",
) {
    val read = onRead != null && content.type != ContentType.BOOK
    val labels = cardLabels()
    val text = remember(content, series, parent, read, isNew, labels, options.itemCountMode) {
        cardText(content, series, read, isNew, labels, options.itemCountMode, parent)
    }
    val caption = if (fitWidth == null) text else {
        val short = cardLabels(short = true)
        val measurer = rememberTextMeasurer()
        val style = MaterialTheme.typography.bodySmall
        // Less the caption's padding.
        val room = with(LocalDensity.current) { (fitWidth - 4.dp).roundToPx() }
        remember(text, content, series, parent, read, isNew, short, options.itemCountMode, measurer, style, room) {
            val subtitle = text.subtitle
            if (subtitle == null || measurer.measure(subtitle, style, maxLines = 1, softWrap = false).size.width <= room) {
                text
            } else {
                cardText(content, series, read, isNew, short, options.itemCountMode, parent)
            }
        }
    }
    val progress = remember(content, labels) { contentProgress(content, labels.progress) }
    val detailsId = (series ?: content).id
    // The cover grows into the page of the very item it shows; a read card with a series opens the series' page instead.
    val hero = if (detailsId == content.id) HeroKey(content.id, LocalHeroOrigin.current + "/" + origin) else null
    val taps = LocalHeroTaps.current
    // One way into the page for the info button and TalkBack's action: it records the hero first.
    val openDetails = {
        hero?.let(taps::tap)
        onOpen(detailsId)
    }
    val details = stringResource(R.string.card_details, (series ?: content).title)
    val downloadLabel = download?.let { downloadBadgeLabel(it) }
    val selectMode = selecting && onToggle != null
    val selectLabel = stringResource(R.string.select_item)
    // A checkbox reads the item, not "Read Vol. 1": a tap selects it.
    val name = if (selectMode && read) {
        remember(content, series, parent, isNew, labels, options.itemCountMode) {
            cardText(content, series, false, isNew, labels, options.itemCountMode, parent).linkLabel
        }
    } else {
        text.linkLabel
    }
    val press = if (selectMode) {
        Modifier.toggleable(selected, role = Role.Checkbox) { onToggle() }
    } else {
        Modifier.combinedClickable(
            role = Role.Button,
            onLongClickLabel = selectLabel,
            onLongClick = onToggle,
        ) {
            if (read) {
                onRead(content.id)
            } else {
                hero?.let(taps::tap)
                onOpen(content.id)
            }
        }
    }
    Column(
        modifier
            .alpha(if (unavailable != null) 0.38f else 1f)
            .then(press)
            .semantics {
                contentDescription = listOfNotNull(name, subtitle.takeIf { text.subtitle == null }, downloadLabel, unavailable).joinToString(", ")
                // The bar itself is cleared below: its label is read after the card's name. A checkbox's state is its own.
                if (!options.hideProgress && !selectMode) progress?.let { stateDescription = it.label }
                // TalkBack's Actions menu lists custom actions, not the long press.
                if (!selectMode) {
                    customActions = listOfNotNull(
                        onToggle?.let { CustomAccessibilityAction(selectLabel) { it(); true } },
                        if (read) CustomAccessibilityAction(details) { openDetails(); true } else null,
                    )
                }
            },
        Arrangement.spacedBy(10.dp),
    ) {
        val status = content.userData?.status
        val glow = highlightReading && !selectMode && !options.hideReadingHighlight && status == ReadingStatus.READING
        val chosen = if (selectMode && selected) Modifier.border(2.dp, MaterialTheme.colorScheme.primary, VoltisShapes.cover) else Modifier
        VCover(coverUrl(content), (if (glow) Modifier.readingGlow() else Modifier).then(chosen), hero.takeIf { !selectMode }) {
            // The card's description names all of these: none is read again.
            val statusIcon = statusIcon(status)
            if (selectMode) {
                if (selected) Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.scrim.copy(alpha = 0.25f)))
                CheckCircle(selected, Modifier.align(Alignment.TopStart).padding(8.dp))
            } else if (statusIcon != null && !options.hideStatus) {
                VBadge(statusIcon, Modifier.align(Alignment.TopStart).padding(8.dp))
            }
            Row(Modifier.align(Alignment.TopEnd).padding(8.dp).clearAndSetSemantics {}, Arrangement.spacedBy(4.dp)) {
                text.newLabel?.let { VBadge(it) }
                if (!options.hideItemCount) text.count?.let { VBadge(it.toString()) }
            }
            // Above the progress bar, clear of the details button.
            if (download != null) DownloadBadgeIcon(download, Modifier.align(Alignment.BottomStart).padding(start = 8.dp, bottom = 10.dp))
            if (progress != null && !options.hideProgress) {
                VProgressBar(
                    progress.fraction,
                    Modifier.align(Alignment.BottomCenter).fillMaxWidth().clearAndSetSemantics {},
                    overlay = true,
                )
            }
            if (read && !selectMode) {
                VIconButton(
                    VIcons.Information,
                    details,
                    openDetails,
                    // The 32 dp circle sits 8 dp from the corner, above the progress bar.
                    Modifier.align(Alignment.BottomEnd).padding(bottom = 4.dp),
                    VIconButtonStyle.Tonal,
                    small = true,
                )
            }
        }
        // A read row keeps the subtitle's line for a card without a series.
        // An item of its series has the web's one-line title over its subtitle.
        if (!options.hideTitle) {
            Caption(
                caption.title,
                caption.subtitle ?: subtitle ?: "".takeIf { onRead != null && parent == null },
                Modifier.clearAndSetSemantics {},
                titleLines = if (series == null && caption.subtitle != null) 1 else 2,
            )
        }
    }
}

/** Select mode's mark on a cover: an empty ring, or filled with a check. */
@Composable
private fun CheckCircle(checked: Boolean, modifier: Modifier = Modifier) {
    val colors = MaterialTheme.colorScheme
    val base = modifier.size(24.dp).clip(CircleShape)
    if (checked) {
        Box(base.background(colors.primary), Alignment.Center) {
            Icon(VIcons.Check, contentDescription = null, Modifier.size(18.dp), tint = colors.onPrimary)
        }
    } else {
        Box(base.background(colors.inverseSurface.copy(alpha = 0.4f)).border(2.dp, colors.inverseOnSurface, CircleShape))
    }
}

/** The two layers of the web's `--shadow-reading`. */
@Composable
private fun Modifier.readingGlow(): Modifier {
    val (inner, outer) = VoltisTheme.colors.readingGlow
    return dropShadow(VoltisShapes.cover, Shadow(radius = outer.blur, color = outer.color, spread = outer.spread))
        .dropShadow(VoltisShapes.cover, Shadow(radius = inner.blur, color = inner.color, spread = inner.spread))
}

@Composable
private fun statusIcon(status: String?): Painter? = when (status) {
    ReadingStatus.READING -> VIcons.BookOpenFilled
    ReadingStatus.COMPLETED -> VIcons.Check
    ReadingStatus.ON_HOLD -> VIcons.PauseFilled
    ReadingStatus.DROPPED -> VIcons.Close
    ReadingStatus.PLAN_TO_READ -> VIcons.BookmarkFilled
    else -> null
}

/**
 * Always two title lines high, plus the subtitle's line when there is one (blank included), so
 * every card of a row is as tall as the next at any font size. A one-line title keeps its
 * subtitle under it: the spare line is left below both.
 */
@Composable
private fun Caption(title: String, subtitle: String?, modifier: Modifier = Modifier, titleLines: Int = 2) {
    val titleStyle = MaterialTheme.typography.bodyMedium.copy(fontWeight = FontWeight.Medium, lineHeight = TitleLineHeight.em)
    val subtitleStyle = MaterialTheme.typography.bodySmall
    // From the font sizes in px, as the text lays out at a non-linear font scale too.
    val minHeight = with(LocalDensity.current) {
        val subtitleLine = if (subtitle == null) 0f else subtitleStyle.fontSize.toPx() * subtitleStyle.lineHeight.value / subtitleStyle.fontSize.value
        (titleStyle.fontSize.toPx() * TitleLineHeight * titleLines + subtitleLine).toDp()
    }
    Column(modifier.heightIn(min = minHeight).padding(horizontal = 2.dp)) {
        Text(title, style = titleStyle, maxLines = titleLines, overflow = TextOverflow.Ellipsis)
        if (subtitle != null) {
            Text(subtitle, color = MaterialTheme.colorScheme.onSurfaceVariant, style = subtitleStyle, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

private const val TitleLineHeight = 1.35f

/**
 * The shape of a card while its row loads, as tall as the row's cards: [subtitle] for a read row,
 * no [title] where titles are hidden.
 */
@Composable
fun ContentCardSkeleton(modifier: Modifier = Modifier, subtitle: Boolean = false, title: Boolean = true) {
    val color = MaterialTheme.colorScheme.surfaceContainerHigh
    Column(modifier, Arrangement.spacedBy(10.dp)) {
        Box(Modifier.fillMaxWidth().aspectRatio(2f / 3f).background(color, VoltisShapes.cover))
        if (title) Box {
            Caption("", "".takeIf { subtitle })
            Box(Modifier.fillMaxWidth(0.7f).height(14.dp).background(color, VoltisShapes.menuItem))
        }
    }
}
