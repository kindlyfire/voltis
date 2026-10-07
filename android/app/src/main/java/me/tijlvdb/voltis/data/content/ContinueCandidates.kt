package me.tijlvdb.voltis.data.content

import me.tijlvdb.voltis.data.api.ReadingStatus
import me.tijlvdb.voltis.data.db.VoltisDatabase
import me.tijlvdb.voltis.data.reading.effectiveReading
import me.tijlvdb.voltis.domain.reading.Recency

/** An item the Continue shortcut could open without the server, and when it was last read or set ([Recency]). */
data class ContinueCandidate(val contentId: String, val type: String, val recency: Recency)

/**
 * What is being read and could open now: the items that can be (a book, a comic with a complete copy) whose
 * effective reading is `reading` and has been read. Identity and copy eligibility come from content and download
 * rows; the reading is the newest server state with the unsent ops over it, whatever the lanes hold.
 */
suspend fun VoltisDatabase.continueCandidates(): List<ContinueCandidate> {
    val rows = snapshots().continuable()
    val readings = effectiveReading(rows.map { it.contentId })
    return rows.mapNotNull { row ->
        val reading = readings[row.contentId]?.takeIf { it.status == ReadingStatus.READING && it.lastReadAt != null } ?: return@mapNotNull null
        ContinueCandidate(row.contentId, row.type, reading.recency ?: return@mapNotNull null)
    }
}
