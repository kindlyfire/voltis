package me.tijlvdb.voltis.domain.catalog

import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType

// A port of `utils/seriesItem.ts`. A continue series has an empty `meta`.

private const val DASHES = "‐‑‒–—―"
private const val MARKS = ":,·\\-$DASHES"
// JavaScript's `\s`, which also takes the no-break and ideographic spaces.
private const val WS = "[\\s\\p{Z}]"
private const val SEP = "[\\s\\p{Z}$MARKS]"

// Mirrors the backend's `bookVolumeRange`.
private const val NUM = """\d+(?:\.\d+)?(?:$WS*[-–~]$WS*\d+(?:\.\d+)?)?"""

private val SEP_RE = Regex(SEP)
private val LEADING_SEP = Regex("^$SEP+")
private val BARE_NUMBER = Regex("^$NUM(?=\$|$WS*[$MARKS])")
private val MARKER = Regex("""^(?:(?:volume|vol|v|chapter|ch)\.?|#)$WS*$NUM(?![\p{L}\p{N}])""", RegexOption.IGNORE_CASE)
private val ONLY_NUM = Regex("^$NUM\$")
private val HAS_TEXT = Regex("""[\p{L}\p{N}]""")

/** Drops sentinels such as calibre's 100000 for specials. */
private fun plausible(n: Double?): Double? = n?.takeIf { it.isFinite() && it >= 0 && it < 10000 }

/** 3 for 3.0, as the web prints numbers. */
private fun Double.label(): String = if (this == toLong().toDouble()) toLong().toString() else toString()

/** The numbering words, as formats taking the number; the UI fills them from string resources. */
data class ItemLabels(
    val volume: String,
    val chapter: String,
    val volumeShort: String,
    val chapterShort: String,
    val issue: String,
)

/** "Volume 3", "Chapter 12", "Vol. 2 · #5"; null when the series has no numbering. */
private fun numberLabel(parts: List<Double?>, series: Content, labels: ItemLabels): String? {
    val vol = plausible(parts.getOrNull(0))?.label()
    if (series.type == ContentType.BOOK_SERIES) return vol?.let(labels.volume::format)
    if (series.type != ContentType.COMIC_SERIES) return null
    val ch = plausible(parts.getOrNull(1))?.label()
    val western = series.meta.kind == "comic"
    return when {
        vol != null && ch != null -> "${labels.volumeShort.format(vol)} · ${(if (western) labels.issue else labels.chapterShort).format(ch)}"
        ch != null -> (if (western) labels.issue else labels.chapter).format(ch)
        vol != null -> (if (western) labels.volumeShort else labels.volume).format(vol)
        else -> null
    }
}

private fun String.points(): List<String> = codePoints().toArray().map { String(Character.toChars(it)) }

private fun String.isSpace() = all(Char::isWhitespace)

private fun fold(ch: String): String = when {
    ch in "’‘‛′" -> "'"
    ch in "“”″" -> "\""
    ch in DASHES -> "-"
    else -> ch.lowercase()
}

/** The rest of [title] after [prefix], or null when it doesn't start with it at a separator. */
private fun afterPrefix(title: List<String>, prefix: List<String>): String? {
    var i = 0
    var j = 0
    while (j < prefix.size) {
        if (i >= title.size) return null
        val ws = prefix[j].isSpace()
        if (ws != title[i].isSpace()) return null
        if (ws) {
            while (j < prefix.size && prefix[j].isSpace()) j++
            while (i < title.size && title[i].isSpace()) i++
            continue
        }
        if (fold(title[i]) != fold(prefix[j])) return null
        i++
        j++
    }
    if (i < title.size && !SEP_RE.containsMatchIn(title[i])) return null
    return title.drop(i).joinToString("")
}

data class SplitTitle(
    val label: String?,
    /** The item title without the series name and, when labeled, its numbering. */
    val stripped: String?,
    /** Whether the title lost its number: the label then repeats nothing the title still says. */
    val removedNumber: Boolean,
)

/** An item's number label and its title shortened against the series. */
fun splitItemTitle(item: Content, series: Content?, labels: ItemLabels): SplitTitle {
    if (series == null) return SplitTitle(null, null, false)
    val label = numberLabel(item.orderParts, series, labels)
    val chars = item.title.points()
    val after = (listOf(series.title) + series.meta.altTitles.orEmpty())
        .map { it.trim().points() }
        .filter { it.isNotEmpty() }
        .sortedByDescending { it.size }
        .firstNotNullOfOrNull { afterPrefix(chars, it) }

    var rest = after?.replace(LEADING_SEP, "") ?: item.title
    val before = rest
    if (label != null) {
        if (after != null) rest = rest.replaceFirst(BARE_NUMBER, "").replace(LEADING_SEP, "")
        while (MARKER.containsMatchIn(rest)) rest = rest.replaceFirst(MARKER, "").replace(LEADING_SEP, "")
    }
    val removedNumber = rest != before
    rest = rest.trim()
    val stripped = rest.takeIf { HAS_TEXT.containsMatchIn(it) && !ONLY_NUM.matches(it) }
    return SplitTitle(label, stripped, removedNumber)
}

/** The item's number over its title on its page, unless the title is nothing but series name and numbering. */
fun itemEyebrow(item: Content, series: Content?, labels: ItemLabels): String? =
    splitItemTitle(item, series, labels).let { if (it.removedNumber && it.stripped == null) null else it.label }

private fun joinName(split: SplitTitle, title: String): String =
    if (split.label != null && split.stripped != null) "${split.label}: ${split.stripped}" else split.label ?: split.stripped ?: title

/** One line naming an item within its series: "Volume 3: The Long Road". */
fun itemName(item: Content, series: Content?, labels: ItemLabels): String =
    joinName(splitItemTitle(item, series, labels), item.title)

/** "Series · Volume 3: Subtitle"; the item title alone when that adds nothing. */
fun readerTitle(item: Content, parent: Content?, labels: ItemLabels): String {
    val split = splitItemTitle(item, parent, labels)
    return if (parent != null && (split.label != null || split.stripped != null)) "${parent.title} · ${joinName(split, item.title)}" else item.title
}
