package me.tijlvdb.voltis.ui

import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import me.tijlvdb.voltis.R
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.domain.catalog.CardLabels
import me.tijlvdb.voltis.domain.catalog.ItemLabels
import me.tijlvdb.voltis.domain.catalog.LengthLabels
import me.tijlvdb.voltis.domain.catalog.ProgressLabels

/**
 * The numbering words of item names ("Volume 3", "Vol. 2 · Ch. 5"), from the string resources.
 * [short] uses the short words throughout ("Vol. 3").
 */
@Composable
fun itemLabels(short: Boolean = false) = ItemLabels(
    volume = stringResource(if (short) R.string.item_volume_short else R.string.item_volume),
    chapter = stringResource(if (short) R.string.item_chapter_short else R.string.item_chapter),
    volumeShort = stringResource(R.string.item_volume_short),
    chapterShort = stringResource(R.string.item_chapter_short),
    issue = stringResource(R.string.item_issue),
)

/** The words of a content card and its progress, from the string resources; [short] as [itemLabels]. */
@Composable
fun cardLabels(short: Boolean = false) = CardLabels(
    item = itemLabels(short),
    progress = ProgressLabels(
        page = stringResource(R.string.progress_page),
        percent = stringResource(R.string.progress_percent),
        read = stringResource(R.string.progress_read),
        dropped = stringResource(R.string.progress_dropped),
    ),
    read = stringResource(R.string.card_read),
    new = stringResource(R.string.card_new),
    newCount = stringResource(R.string.card_new_count),
    caughtUp = stringResource(R.string.card_caught_up),
    unread = stringResource(R.string.card_unread),
    items = stringResource(R.string.card_items),
    statuses = statusLabels().toMap(),
)

/** A reading status's name; null for a status this app doesn't know. */
@StringRes
fun statusRes(status: String) = when (status) {
    ReadingStatus.READING -> R.string.status_reading
    ReadingStatus.COMPLETED -> R.string.status_completed
    ReadingStatus.ON_HOLD -> R.string.status_on_hold
    ReadingStatus.DROPPED -> R.string.status_dropped
    ReadingStatus.PLAN_TO_READ -> R.string.status_plan_to_read
    else -> null
}

/** The reading statuses with their names, in the web's order. */
@Composable
fun statusLabels() = listOf(
    ReadingStatus.READING,
    ReadingStatus.COMPLETED,
    ReadingStatus.ON_HOLD,
    ReadingStatus.DROPPED,
    ReadingStatus.PLAN_TO_READ,
).map { it to stringResource(checkNotNull(statusRes(it))) }

/** The words of a length summary ("1,212 pages · 6 h 45 min"), from the string resources. */
@Composable
fun lengthLabels() = LengthLabels(
    words = stringResource(R.string.length_words),
    page = stringResource(R.string.length_page),
    pages = stringResource(R.string.length_pages),
    underMinute = stringResource(R.string.length_under_minute),
    minutes = stringResource(R.string.length_minutes),
    hours = stringResource(R.string.length_hours),
    hoursMinutes = stringResource(R.string.length_hours_minutes),
    total = stringResource(R.string.length_total),
    left = stringResource(R.string.length_left),
)

@Composable
fun typeLabel(type: String) = when (type) {
    ContentType.COMIC -> stringResource(R.string.type_comic)
    ContentType.COMIC_SERIES -> stringResource(R.string.type_comic_series)
    ContentType.BOOK -> stringResource(R.string.type_book)
    ContentType.BOOK_SERIES -> stringResource(R.string.type_book_series)
    else -> type
}
