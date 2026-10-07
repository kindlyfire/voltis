package me.tijlvdb.voltis.domain.catalog

import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withPermit
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ReadingStatus

/** What a batch does, for its summary. A null [status] clears the status and keeps the position. */
sealed interface BulkWhat {
    data class Status(val status: String?) : BulkWhat

    /** Status, position and reading time. */
    data object Clear : BulkWhat
}

/** One guarded reading command for one item (decision 11), never the unguarded bulk endpoint. */
sealed interface BulkCommand {
    val item: Selected

    data class SetStatus(override val item: Selected, val status: String?) : BulkCommand

    data class CompleteSeries(override val item: Selected, val includeUnread: Boolean) : BulkCommand

    data class Clear(override val item: Selected) : BulkCommand
}

/** P4 §6's table: Completed completes a series through its own command; anything else sets the status. */
fun statusPlan(items: Collection<Selected>, status: String?, includeUnread: Boolean): List<BulkCommand> = items.map {
    if (status == ReadingStatus.COMPLETED && it.content.isSeries) BulkCommand.CompleteSeries(it, includeUnread) else BulkCommand.SetStatus(it, status)
}

fun clearPlan(items: Collection<Selected>): List<BulkCommand> = items.map { BulkCommand.Clear(it) }

/** Series expanded at once. */
const val EXPAND_SERIES_AT_ONCE = 4

/** More volumes than this can't be queued by one bulk download. */
const val MAX_BULK_DOWNLOADS = 500

/**
 * What a bulk download offers (P4 §6): the [offered] volumes, as the rows were when counted; the
 * download itself reconciles them again. [already] counts the volumes left out because they are
 * downloaded, gone from the server or on their way; [books] the skipped items.
 */
data class DownloadPlan(
    val offered: List<Content>,
    val already: Int,
    val books: Int,
    val bytes: Long,
    /** Some offered volume has no size, so [bytes] is a lower bound. */
    val sizeUnknown: Boolean,
) {
    val count get() = offered.size
    val tooMany get() = count > MAX_BULK_DOWNLOADS
}

/**
 * Expands the selected series to their comic volumes ([volumes] answers a series' list; [EXPAND_SERIES_AT_ONCE]
 * at a time; a failure fails the plan) and offers every one that is not [present] (asked once, after the
 * expansion): the store's own classification of the rows, by state and staleness. Books are skipped and counted.
 */
suspend fun downloadPlan(
    items: Collection<Selected>,
    present: suspend () -> Set<String>,
    volumes: suspend (seriesId: String) -> List<Content>,
): DownloadPlan {
    val gate = Semaphore(EXPAND_SERIES_AT_ONCE)
    val expanded = coroutineScope {
        items.map { item ->
            async {
                val c = item.content
                when (c.type) {
                    ContentType.COMIC -> listOf(c)
                    ContentType.COMIC_SERIES -> gate.withPermit { volumes(c.id) }.filter { it.type == ContentType.COMIC }
                    else -> null
                }
            }
        }.map { it.await() }
    }
    val books = expanded.count { it == null }
    val all = expanded.filterNotNull().flatten().distinctBy { it.id }
    val skip = present()
    val (already, offered) = all.partition { it.id in skip }
    return DownloadPlan(
        offered, already.size, books,
        bytes = offered.sumOf { it.fileSize ?: 0L },
        sizeUnknown = offered.any { it.fileSize == null },
    )
}
