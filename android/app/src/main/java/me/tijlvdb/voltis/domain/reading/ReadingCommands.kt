package me.tijlvdb.voltis.domain.reading

import me.tijlvdb.voltis.data.api.Content

/**
 * The reading writes of content pages, through the outbox (P2 §6). Each takes the [Content] its
 * page shows: the item's lane is seeded from its stored snapshot (the content's own reading is
 * the fallback) before the command is made. `QUEUED` means stored and sent later; a refusal or a drop throws with its message.
 */
interface ReadingCommands {
    /** A null [status] removes the status and keeps the saved position. */
    suspend fun setStatus(content: Content, status: String?): CommandOutcome

    /** Removes status, position and last-read time; for a series, its volumes' too. */
    suspend fun clear(content: Content): CommandOutcome

    suspend fun markSeriesCompleted(series: Content, includeUnread: Boolean): CommandOutcome

    /** Completes the series' volumes up to and including [untilId]. */
    suspend fun markThrough(series: Content, untilId: String): CommandOutcome
}
