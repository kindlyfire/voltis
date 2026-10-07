package me.tijlvdb.voltis.domain.catalog

import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ReadingStatus

// The texts of a content card, a port of the computed values of `components/ContentGrid/Item.vue`.

/** The card's words, as formats where they take a value; the UI fills them from string resources. */
data class CardLabels(
    val item: ItemLabels,
    val progress: ProgressLabels,
    /** "Read Ember Saga": a card that opens the reader. */
    val read: String,
    val new: String,
    /** "2 new". */
    val newCount: String,
    /** "Reading · Caught up": takes the status. */
    val caughtUp: String,
    /** "3 unread". */
    val unread: String,
    /** "3 items": the count in [ItemCountMode.Total]. */
    val items: String,
    /** By [ReadingStatus]. */
    val statuses: Map<String, String>,
)

data class CardText(
    /** The caption's title: the series', an item's label or shortened title within [cardText]'s parent, or its own. */
    val title: String,
    val subtitle: String?,
    val newLabel: String?,
    val statusLabel: String?,
    /** A series' unread or total children; null hides the badge. */
    val count: Int?,
    /** Names the whole card, so badges and the progress bar needn't be read. */
    val linkLabel: String,
)

/**
 * [series] shows [content] as the item to read next in it: the card is titled by the series, with
 * the item under it. [parent] shows it as an item of that series, by its number and shortened
 * title. [read] is a card that opens the reader; [isNew] an item added since the user caught up
 * with its series. [countMode] picks the series count; an unread count of 0 is left out.
 */
fun cardText(
    content: Content,
    series: Content?,
    read: Boolean,
    isNew: Boolean,
    labels: CardLabels,
    countMode: ItemCountMode = ItemCountMode.Unread,
    parent: Content? = null,
): CardText {
    val split = (series ?: parent)?.let { splitItemTitle(content, it, labels.item) }
    val readLabel = series?.let { seriesReadLabel(it, labels.progress) }
    // The label over the stripped title, or the stripped title alone.
    val line1 = split?.label ?: split?.stripped ?: content.title
    val title = series?.title ?: line1
    val subtitle = when {
        series == null -> split?.stripped?.takeIf { split.label != null }
        readLabel != null -> "$line1 · $readLabel"
        else -> line1
    }

    val status = content.userData?.status
    // A completed series counts what was added since; an item is new to its series.
    val added = content.newChildrenCount ?: 0
    val newLabel = when {
        isNew -> labels.new
        status == ReadingStatus.COMPLETED && added > 0 -> labels.newCount.format(added)
        else -> null
    }
    val total = content.childrenCount ?: 0
    val caughtUp = total > 0 && (content.completedChildrenCount ?: 0) + (content.droppedChildrenCount ?: 0) == total
    val statusLabel = status?.let { labels.statuses[it] ?: it }
        ?.let { if (status == ReadingStatus.READING && caughtUp) labels.caughtUp.format(it) else it }
    val unread = countMode == ItemCountMode.Unread
    val count = (if (unread) content.unreadChildrenCount?.takeIf { it != 0 } else content.childrenCount).takeIf { content.isSeries }

    // The label only adds something when the title doesn't already hold the number.
    val itemTitle = if (split?.label != null && !split.removedNumber) "${split.label}, ${content.title}" else content.title
    val name = series?.title ?: itemTitle
    val parts = buildList {
        add(if (read) labels.read.format(name) else name)
        if (series != null) {
            add(itemTitle)
            readLabel?.let(::add)
        }
        newLabel?.let(::add)
        statusLabel?.let(::add)
        count?.let { add((if (unread) labels.unread else labels.items).format(it)) }
    }
    return CardText(title, subtitle, newLabel, statusLabel, count, parts.joinToString(", "))
}
