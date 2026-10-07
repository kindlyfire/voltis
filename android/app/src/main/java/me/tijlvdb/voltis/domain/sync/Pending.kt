package me.tijlvdb.voltis.domain.sync

import kotlinx.coroutines.flow.Flow
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.intOrNull
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.UserData

/** What a list entry is unique by on the server (P4 §9): an entry refers to its item by library and `uri`. */
data class EntryKey(val libraryId: String, val uri: String)

/** An item as a list edit names it: its content ID, its entry key, and what a list's page shows of it. */
data class EntryItem(val contentId: String, val key: EntryKey, val title: String, val type: String, val coverVersion: String?)

/** What a star or rating submit names (P4 §8): [title] is what Needs attention and a notice show. */
data class UserDataItem(val contentId: String, val libraryId: String?, val uri: String?, val title: String)

/** The server's answer to the last write of an item that landed, and its place in the owner's sequence. */
data class LandedUserData(val seq: Long, val starred: Boolean, val rating: Int?)

/** Stuck from this many failed runs on (P4 decision 10). */
const val STUCK_ATTEMPTS = 3

/**
 * One item's pending star and rating (P4 §7). A [wanted] row holds the wish to send: [starred] null is
 * "not changed", [ratingSet] with a null [rating] is "cleared". [rev] rises with every merge, and a run
 * clears the wish only at the one it sent. [landed] is kept for the content page.
 */
data class PendingUserData(
    val contentId: String,
    val libraryId: String?,
    val uri: String?,
    val title: String,
    val wanted: Boolean,
    val starred: Boolean?,
    val ratingSet: Boolean,
    val rating: Int?,
    val rev: Long,
    val attempts: Int,
    val lastError: String?,
    val createdAt: Long,
    val landed: LandedUserData?,
) {
    val stuck get() = wanted && attempts >= STUCK_ATTEMPTS
}

/**
 * A submit merged into the item's row: a given field replaces the row's wish for it, the other keeps it.
 * [rating]: null leaves it, [JsonNull] clears it. The landed values are kept.
 */
fun PendingUserData?.merge(item: UserDataItem, starred: Boolean?, rating: JsonElement?, now: Long): PendingUserData {
    val held = this?.takeIf { it.wanted }
    val ratingSet = rating != null || held?.ratingSet == true
    return PendingUserData(
        item.contentId, item.libraryId, item.uri, item.title, wanted = true,
        starred = starred ?: held?.starred,
        ratingSet = ratingSet,
        rating = if (rating != null) (rating as? JsonPrimitive)?.takeIf { it !is JsonNull }?.intOrNull else held?.rating,
        rev = (this?.rev ?: 0) + 1, attempts = 0, lastError = null, createdAt = held?.createdAt ?: now,
        landed = this?.landed,
    )
}

/** Whether [change] is a drop of this row's wish at its current `rev`: only then its notice is stored. */
fun PendingUserData?.drops(change: PendingChange.Dropped) = this != null && wanted && rev == change.sentRev

/**
 * What [change] does to the item's row (P4 §7), for the store to write in one transaction; null is no row.
 * [nextSeq] is `max(landed_seq) + 1`, used by a landing. A wish is cleared only at the `rev` a run sent; a row
 * without a wish is deleted unless it holds landed values.
 */
fun PendingUserData?.after(change: PendingChange, nextSeq: Long): PendingUserData? {
    val row = this
    fun PendingUserData.cleared() = copy(wanted = false, starred = null, ratingSet = false, rating = null, attempts = 0, lastError = null)
    fun PendingUserData.settled() = takeIf { landed != null }
    return when (change) {
        is PendingChange.Submit -> row.merge(change.item, change.starred, change.rating, change.now)
        is PendingChange.Landed -> row?.copy(landed = LandedUserData(nextSeq, change.answer.starred, change.answer.rating))
            ?.let { if (it.rev == change.sentRev) it.cleared() else it.copy(attempts = 0) }
        is PendingChange.Failed -> if (row != null && row.wanted && row.rev == change.sentRev) row.copy(attempts = row.attempts + 1, lastError = change.error) else row
        is PendingChange.Dropped -> if (row.drops(change)) row!!.cleared().settled() else row
        is PendingChange.Reset -> if (row != null && row.wanted) row.copy(attempts = 0) else row
        is PendingChange.Discard -> if (row != null && row.wanted) row.cleared().settled() else row
    }
}

/**
 * The page's star and rating (P4 §8): the loaded ones, then the landed answer when it is newer than the
 * load ([loadedAt] is the `landedSeq` read before the load), then the wish.
 */
fun Content.withSync(row: PendingUserData?, loadedAt: Long): Content {
    if (row == null) return this
    var starred = userData?.starred ?: false
    var rating = userData?.rating
    row.landed?.takeIf { it.seq > loadedAt }?.let {
        starred = it.starred
        rating = it.rating
    }
    if (row.wanted) {
        row.starred?.let { starred = it }
        if (row.ratingSet) rating = row.rating
    }
    return copy(userData = (userData ?: UserData()).copy(starred = starred, rating = rating))
}

/** What a content page needs of the pending star and rating of its account. */
interface UserDataSync {
    /** Returns once stored. Throws `StorageFullException` when it can't be, `SyncUnavailable` without the account's owner. */
    suspend fun setUserData(account: String, item: UserDataItem, starred: Boolean? = null, rating: JsonElement? = null)

    /** The item's row, wish or landed; null while no owner of [account] is bound. */
    fun pending(account: String, contentId: String): Flow<PendingUserData?>

    /** `max(landed_seq)`: a load's start stamp. Throws `SyncUnavailable` without the account's owner. */
    suspend fun landedSeq(account: String): Long
}

/** A pending row's [PendingUserData.lastError] for an answer the app couldn't read: a kind, not text, so the attention row renders it in the current language. */
const val UNREADABLE_ERROR = "unreadable_response"
