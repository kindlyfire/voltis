package me.tijlvdb.voltis.domain.catalog

import kotlin.math.roundToInt
import kotlinx.serialization.json.JsonObject
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.domain.numberOrNull

// A port of `utils/contentProgress.ts`.

/** The progress texts, as formats; the UI fills them from string resources. */
data class ProgressLabels(
    /** "Page 13 / 140". */
    val page: String,
    /** "9%". */
    val percent: String,
    /** "11/12 read". */
    val read: String,
    /** "11/12 read · 1 dropped": the read label, then the count. */
    val dropped: String,
)

data class ContentProgress(val fraction: Float, val label: String)

private fun JsonObject?.number(name: String): Double? = this?.get(name).numberOrNull()

private fun percentProgress(percent: Double?, labels: ProgressLabels): ContentProgress? {
    if (percent == null) return null
    val clamped = percent.coerceIn(0.0, 100.0)
    // Only the ends round to 0% and 100%.
    val shown = if (clamped > 0 && clamped < 100) clamped.roundToInt().coerceIn(1, 99) else clamped.toInt()
    return ContentProgress((clamped / 100).toFloat(), labels.percent.format(shown))
}

/**
 * Null hides the bar: no saved position, completed or dropped, or not enough data. A saved start
 * or end still shows.
 */
fun contentProgress(content: Content, labels: ProgressLabels): ContentProgress? {
    val status = content.userData?.status
    if (status == ReadingStatus.COMPLETED || status == ReadingStatus.DROPPED) return null
    val progress = content.userData?.progress
    return when (content.type) {
        ContentType.COMIC -> {
            val page = progress.number("current_page") ?: return null
            val pages = content.fileData.pages?.size ?: 0
            // List rows come without pages: fall back to the percent the reader writes.
            if (pages <= 0) return percentProgress(progress.number("progress_percent"), labels)
            // As the reader counts and clamps it: the pages before this one are read.
            val shown = page.toInt().coerceIn(0, pages - 1)
            ContentProgress(shown.toFloat() / pages, labels.page.format(shown + 1, pages))
        }
        ContentType.BOOK -> percentProgress(progress.number("progress_percent"), labels)
        else -> {
            val total = content.childrenCount ?: 0
            // Done with (completed or dropped), as the unread count leaves out.
            val read = total - (content.unreadChildrenCount ?: 0)
            val label = seriesReadLabel(content, labels)
            if (label == null || read <= 0 || read >= total) null else ContentProgress(read.toFloat() / total, label)
        }
    }
}

/**
 * The bar under a content page's cover. Unlike a card's, a series' bar stays at 100% and whatever
 * its status: it carries the read count. [caughtUp] is the "%s · Caught up" format.
 */
fun coverProgress(content: Content, labels: ProgressLabels, caughtUp: String): ContentProgress? {
    if (!content.isSeries) return contentProgress(content, labels)
    val total = content.childrenCount ?: 0
    val read = total - (content.unreadChildrenCount ?: 0)
    val label = seriesReadLabel(content, labels)
    if (label == null || read <= 0) return null
    val done = read >= total && content.userData?.status != ReadingStatus.COMPLETED
    return ContentProgress(read.toFloat() / total, if (done) caughtUp.format(label) else label)
}

/** "11/12 read · 1 dropped"; null for a series without children. */
fun seriesReadLabel(series: Content, labels: ProgressLabels): String? {
    val total = series.childrenCount ?: 0
    if (total <= 0) return null
    val dropped = series.droppedChildrenCount ?: 0
    val label = labels.read.format(series.completedChildrenCount ?: 0, total)
    return if (dropped > 0) labels.dropped.format(label, dropped) else label
}
