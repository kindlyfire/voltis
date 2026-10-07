package me.tijlvdb.voltis.domain.catalog

import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.time.format.DateTimeParseException
import java.time.format.FormatStyle
import java.util.Locale
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.MetadataLink

// The details rows of a content page: `details` in `pages/content/InfoHeader.vue`, read-only.

enum class DetailField { Staff, Publishers, Published, Length, Genres, Tags, Links }

/** A value of a row: [suffix] (a staff member's role) follows it as text; [facet] is its Discover page, if it has one. */
data class DetailItem(val text: String, val suffix: String = "", val facet: FacetRef? = null)

/** One row: its [text]; for Staff, Publishers, Genres and Tags its [items]; for [DetailField.Links] its [links]. */
data class DetailRow(
    val field: DetailField,
    val text: String = "",
    val items: List<DetailItem> = emptyList(),
    val links: List<MetadataLink> = emptyList(),
)

/** The rows that have a value, in the web's order. [length] is the summary, when the content has a length. */
fun detailRows(content: Content, length: String?, locale: Locale = Locale.getDefault()): List<DetailRow> = buildList {
    val meta = content.meta
    val keys = content.facetKeys
    /** [keys] are aligned with the values, index for index; without them nothing links. */
    fun row(field: DetailField, kind: String, items: List<DetailItem>?, keys: List<String?>?) {
        if (items.isNullOrEmpty()) return
        val linked = items.mapIndexed { i, item -> item.copy(facet = keys?.getOrNull(i)?.let { FacetRef(kind, it) }) }
        add(DetailRow(field, linked.joinToString(", ") { it.text + it.suffix }, linked))
    }
    row(DetailField.Staff, FacetKind.PEOPLE, meta.staff?.map { DetailItem(it.name, " (${it.role})") }, keys?.staff)
    row(DetailField.Publishers, FacetKind.PUBLISHERS, meta.publishers?.map(::DetailItem), keys?.publishers)
    meta.publicationDate?.takeIf { it.isNotEmpty() }?.let { add(DetailRow(DetailField.Published, formatDate(it, locale))) }
    length?.let { add(DetailRow(DetailField.Length, it)) }
    row(DetailField.Genres, FacetKind.GENRES, meta.genres?.map { DetailItem(slugLabel(it)) }, keys?.genres)
    row(DetailField.Tags, FacetKind.TAGS, meta.tags?.map(::DetailItem), keys?.tags)
    // Only what a browser opens: another scheme (a `file:` URL, say) can't be handed to one.
    meta.links?.filter { WEB_URL.containsMatchIn(it.url) }?.takeIf { it.isNotEmpty() }?.let { add(DetailRow(DetailField.Links, links = it)) }
}

/** "Slice of life" for `slice_of_life`. */
fun slugLabel(slug: String) = slug.replace('_', ' ').replaceFirstChar { it.uppercase() }

private val WEB_URL = Regex("^https?://", RegexOption.IGNORE_CASE)

private val PARTIAL_DATE = Regex("""^\d{4}(-\d{2})?$""")

/** A full date in words, in UTC so the day doesn't shift; a year or year-month stays as is (a day would be made up). */
fun formatDate(value: String, locale: Locale = Locale.getDefault()): String {
    if (PARTIAL_DATE.matches(value)) return value
    val date = try {
        LocalDate.parse(value)
    } catch (_: DateTimeParseException) {
        try {
            OffsetDateTime.parse(value).withOffsetSameInstant(ZoneOffset.UTC).toLocalDate()
        } catch (_: DateTimeParseException) {
            return value
        }
    }
    return DateTimeFormatter.ofLocalizedDate(FormatStyle.LONG).withLocale(locale).format(date)
}

/** The date of a moment (a timestamp with an offset) in words, as the calendar in [zone] has it; anything else as [formatDate]. */
fun formatTimestampDate(value: String, zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String {
    val date = try {
        OffsetDateTime.parse(value).atZoneSameInstant(zone).toLocalDate()
    } catch (_: DateTimeParseException) {
        return formatDate(value, locale)
    }
    return DateTimeFormatter.ofLocalizedDate(FormatStyle.LONG).withLocale(locale).format(date)
}
