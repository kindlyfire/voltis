package me.tijlvdb.voltis.domain.catalog

import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ReadingStatus

// What a content page offers for a content type.

val Content.isSeries get() = type == ContentType.COMIC_SERIES || type == ContentType.BOOK_SERIES

/** Books can't be read yet: their pages show a note instead of the continue button. */
val Content.isReadable get() = type == ContentType.COMIC || type == ContentType.COMIC_SERIES

/** A series' children are volumes for books and chapters for comics, as the web's `childNoun`. */
val Content.hasVolumes get() = type == ContentType.BOOK_SERIES

/** Completing a series with unread children asks whether they are read too. */
fun Content.completingAsks(status: String?) = status == ReadingStatus.COMPLETED && isSeries && (unreadChildrenCount ?: 0) > 0

/** A caught-up series that isn't completed yet: the page offers to complete it. */
val Content.offersCompleting
    get() = isSeries && (childrenCount ?: 0) > 0 && (unreadChildrenCount ?: 0) == 0 && userData?.status != ReadingStatus.COMPLETED
