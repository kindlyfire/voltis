package me.tijlvdb.voltis.domain.catalog

import java.util.Locale
import kotlin.math.roundToLong
import me.tijlvdb.voltis.data.api.ContentLength

// A port of `utils/readingTime.ts`.

/** The length texts, as formats; the UI fills them from string resources. */
data class LengthLabels(
    /** "82K words": takes the compact count. */
    val words: String,
    /** "1 page". */
    val page: String,
    /** "1,212 pages": takes the grouped count. */
    val pages: String,
    /** "< 1 min". */
    val underMinute: String,
    /** "45 min". */
    val minutes: String,
    /** "5 h". */
    val hours: String,
    /** "5 h 30 min". */
    val hoursMinutes: String,
    /** "212 pages · 1 h 10 min": the count, then the time. */
    val total: String,
    /** "82K words · 3 h 10 min left". */
    val left: String,
)

/** Coarser the longer it gets. Buckets go by the rounded value, so 59.7 min is "1 h". */
fun formatDuration(minutes: Double, labels: LengthLabels): String {
    if (minutes < 1) return labels.underMinute
    val rounded = minutes.roundToLong()
    if (rounded < 60) return labels.minutes.format(rounded)
    val fives = (minutes / 5).roundToLong() * 5
    if (fives >= 600) return labels.hours.format((minutes / 60).roundToLong())
    val h = fives / 60
    val m = fives % 60
    return if (m > 0) labels.hoursMinutes.format(h, m) else labels.hours.format(h)
}

/**
 * "950", "12.3K", "1.3M": `Intl.NumberFormat` compact notation with one fraction digit, which
 * neither the JVM tests' Java nor minSdk 28 has a formatter for.
 */
fun compactNumber(n: Int): String {
    if (n < 1000) return n.toString()
    var unit = 1000L
    var suffix = 0
    // Tenths of the unit, rounded half up. 999,950 rounds to 1000.0K, which is 1M.
    fun tenths() = (n * 10L + unit / 2) / unit
    while (tenths() >= 10_000 && suffix < 2) {
        unit *= 1000
        suffix++
    }
    val tenths = tenths()
    val fraction = if (tenths % 10 == 0L) "" else ".${tenths % 10}"
    return "${tenths / 10}$fraction${"KMB"[suffix]}"
}

/** "82K words · 3 h 10 min left". The speeds are the user's, or the defaults. */
fun lengthSummary(length: ContentLength, wordsPerMinute: Double, secondsPerPage: Double, labels: LengthLabels): String {
    val words = length.unit == "words"
    val count = when {
        words -> labels.words.format(compactNumber(length.total))
        length.total == 1 -> labels.page
        else -> labels.pages.format("%,d".format(Locale.ENGLISH, length.total))
    }
    fun minutes(n: Int) = if (words) n / wordsPerMinute else n * secondsPerPage / 60
    return if (length.remaining == 0 || length.remaining == length.total) {
        labels.total.format(count, formatDuration(minutes(length.total), labels))
    } else {
        labels.left.format(count, formatDuration(minutes(length.remaining), labels))
    }
}
