package me.tijlvdb.voltis.ui.content

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ProvideTextStyle
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.serialization.json.JsonNull
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.style.LineHeightStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.UserPrefs
import me.tijlvdb.voltis.domain.catalog.DetailField
import me.tijlvdb.voltis.domain.catalog.DetailRow
import me.tijlvdb.voltis.domain.catalog.FacetRef
import me.tijlvdb.voltis.domain.catalog.detailRows
import me.tijlvdb.voltis.data.downloads.isDetail
import me.tijlvdb.voltis.domain.catalog.lengthSummary
import me.tijlvdb.voltis.ui.kit.VButton
import me.tijlvdb.voltis.ui.kit.VButtonStyle
import me.tijlvdb.voltis.ui.kit.VIcons
import me.tijlvdb.voltis.ui.lengthLabels
import me.tijlvdb.voltis.ui.openInBrowser

/** The description, four lines with Show more when that hides text. [expanded] is Show more's state. */
@Composable
fun DescriptionBlock(content: Content, expanded: MutableState<Boolean>, modifier: Modifier = Modifier) {
    val description = content.meta.description?.takeIf { it.isNotBlank() } ?: return
    ProvideTextStyle(MaterialTheme.typography.bodyMedium) { Description(description, expanded, modifier) }
}

/**
 * What a series' Details tab holds: the description and the rows. [loaded]: the detail response (or the stored copy) has
 * been seen, so an empty Details is empty for good and not just waiting.
 */
class Details(val rows: List<DetailRow>, val description: String?, val loaded: Boolean)

@Composable
fun rememberDetails(content: Content, vm: ContentViewModel): Details {
    val me by vm.me.collectAsStateWithLifecycle()
    val prefs = me?.prefs ?: UserPrefs(JsonNull)
    val labels = lengthLabels()
    val rows = remember(content, prefs.wordsPerMinute, prefs.secondsPerPage, labels) {
        detailRows(content, content.length?.let { lengthSummary(it, prefs.wordsPerMinute, prefs.secondsPerPage, labels) })
    }
    val description = content.meta.description?.takeIf { it.isNotBlank() }
    val loaded = vm.ready || content.isDetail || vm.cached || vm.error != null
    return remember(rows, description, loaded) { Details(rows, description, loaded) }
}

/** The Details tab: the description, then the rows; a line saying so while there are none. */
@Composable
fun DetailsPanel(content: Content, details: Details, expanded: MutableState<Boolean>, openFacet: (FacetRef) -> Unit, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth()) {
        if (details.description != null) DescriptionBlock(content, expanded, Modifier.padding(bottom = 16.dp))
        DetailRows(details.rows, openFacet)
        if (details.rows.isEmpty() && (!details.loaded || details.description == null)) {
            Text(
                stringResource(if (details.loaded) R.string.detail_none else R.string.loading),
                Modifier.padding(vertical = 10.dp, horizontal = 4.dp),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodyMedium,
            )
        }
    }
}

/** The rows, read-only: label and value, as the web's list. Linked values open their Discover page. */
@Composable
fun DetailRows(rows: List<DetailRow>, openFacet: (FacetRef) -> Unit, modifier: Modifier = Modifier) {
    if (rows.isEmpty()) return
    ProvideTextStyle(MaterialTheme.typography.bodyMedium) {
        Column(modifier.fillMaxWidth()) { for (row in rows) Detail(row, openFacet) }
    }
}

/** Four lines, with Show more when that hides text. */
@Composable
private fun Description(text: String, expandedState: MutableState<Boolean>, modifier: Modifier) {
    var expanded by expandedState
    // Saved with the item: it is the same height the first frame it is back, so nothing above the screen shifts when it returns to it.
    var clamped by rememberSaveable(text) { mutableStateOf(false) }
    Column(modifier.fillMaxWidth()) {
        Text(
            text,
            maxLines = if (expanded) Int.MAX_VALUE else 4,
            overflow = TextOverflow.Ellipsis,
            onTextLayout = { if (!expanded) clamped = it.hasVisualOverflow },
        )
        if (clamped || expanded) {
            VButton(
                stringResource(if (expanded) R.string.show_less else R.string.show_more),
                { expanded = !expanded },
                // The button's text lines up with the description's.
                Modifier.offset(x = (-12).dp),
                VButtonStyle.Text,
            )
        }
    }
}

/**
 * Linked values are comma-separated text links, as on the web. Each link is a run of its own in one text, so TalkBack lists
 * them as links of that text and a keyboard tabs through them.
 */
@Composable
private fun Detail(row: DetailRow, openFacet: (FacetRef) -> Unit) {
    val colors = MaterialTheme.colorScheme
    HorizontalDivider(color = colors.outlineVariant)
    val plain = row.links.isEmpty() && row.items.none { it.facet != null }
    val merge = if (plain) Modifier.semantics(mergeDescendants = true) {} else Modifier
    // Grows with the font size, so a label isn't broken mid-word at the largest sizes.
    val labelWidth = (96.dp * LocalDensity.current.fontScale).coerceAtMost(128.dp)
    val linked = row.items.any { it.facet != null }
    // Linked values are text, not 48 dp targets: a tall line keeps neighbours (and wrapped lines) apart, so no two tap areas overlap.
    val style = if (linked) {
        MaterialTheme.typography.bodyMedium.copy(lineHeight = LinkLineHeight, lineHeightStyle = LineHeightStyle(LineHeightStyle.Alignment.Center, LineHeightStyle.Trim.None))
    } else {
        MaterialTheme.typography.bodyMedium
    }
    Row(merge.fillMaxWidth().padding(horizontal = 4.dp, vertical = if (linked) 4.dp else 10.dp), Arrangement.spacedBy(16.dp)) {
        Text(stringResource(row.field.label), Modifier.width(labelWidth), style = style)
        if (row.links.isNotEmpty()) {
            val context = LocalContext.current
            // The button's own 12 dp padding comes off, so the first link lines up with the other values.
            FlowRow(Modifier.weight(1f).offset(x = (-12).dp)) {
                for (link in row.links) {
                    val name = stringResource(R.string.detail_link, link.label)
                    VButton(
                        link.label,
                        { context.openInBrowser(link.url) },
                        Modifier.semantics { contentDescription = name },
                        VButtonStyle.Text,
                        icon = VIcons.OpenInNew,
                    )
                }
            }
        } else if (row.items.isEmpty()) {
            Text(row.text, Modifier.weight(1f), color = colors.onSurfaceVariant)
        } else {
            val muted = colors.onSurfaceVariant
            val link = TextLinkStyles(
                SpanStyle(color = colors.primary, fontWeight = FontWeight.Medium),
                focusedStyle = SpanStyle(color = colors.primary, fontWeight = FontWeight.Medium, background = colors.primary.copy(alpha = 0.16f)),
                pressedStyle = SpanStyle(color = colors.primary, fontWeight = FontWeight.Medium, background = colors.primary.copy(alpha = 0.16f)),
            )
            val text = remember(row, link, muted) {
                buildAnnotatedString {
                    row.items.forEachIndexed { i, item ->
                        if (i > 0) append(", ")
                        val facet = item.facet
                        if (facet == null) {
                            append(item.text)
                        } else {
                            withLink(LinkAnnotation.Clickable(item.text, link) { openFacet(facet) }) { append(item.text) }
                        }
                        append(item.suffix)
                    }
                }
            }
            Text(text, Modifier.weight(1f), color = muted, style = style)
        }
    }
}

private val LinkLineHeight = 30.sp

private val DetailField.label
    get() = when (this) {
        DetailField.Staff -> R.string.detail_staff
        DetailField.Publishers -> R.string.detail_publishers
        DetailField.Published -> R.string.detail_published
        DetailField.Length -> R.string.detail_length
        DetailField.Genres -> R.string.detail_genres
        DetailField.Tags -> R.string.detail_tags
        DetailField.Links -> R.string.detail_links
    }
