package me.tijlvdb.voltis.domain.catalog

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.domain.reading.EffectiveReading
import me.tijlvdb.voltis.domain.reading.Stamps
import org.junit.Assert.assertEquals
import org.junit.Test

class OfflineContinueTest {
    private val noProgress = JsonObject(emptyMap())

    /** A volume as the effective reading shows it. [rank]s are queue places of unsent ops; [readAt] and [statusAt] the server's times. */
    private fun vol(
        n: Int, status: String? = null, readAt: String? = null, statusAt: String? = null, page: Int? = null, readRank: Int? = null, statusRank: Int? = null,
    ) = VolumeReading(
        Content("v$n", "Vol. $n", ContentType.COMIC, parentId = "s"),
        EffectiveReading(
            status, page?.let { JsonObject(mapOf("current_page" to JsonPrimitive(it))) } ?: noProgress, readAt ?: readRank?.let { "unsent" },
            Stamps(readAt, readRank, statusAt, statusRank), seq = 1,
        ),
    )

    private fun pick(vararg volumes: VolumeReading) = offlineContinue("s", volumes.toList()).let { listOf(it.target?.id, it.action, it.reason, it.earlierUnreadId) }

    private fun done(n: Int, at: String) = vol(n, "completed", at, at)

    /** The backend's rules: start at the first volume, resume the one read last, move on from a finished one, then the fallbacks in the server's order. */
    @Test
    fun picksWhatTheServerWould() {
        assertEquals(listOf("v1", "start", null, null), pick(vol(1), vol(2)))
        assertEquals(listOf("v2", "resume", null, null), pick(done(1, "2026-01-01T00:00:00Z"), vol(2, "reading", "2026-01-02T00:00:00Z", page = 4), vol(3)))
        assertEquals(listOf("v3", "next", null, null), pick(done(1, "2026-01-01T00:00:00Z"), done(2, "2026-01-02T00:00:00Z"), vol(3)))
        // The latest read or status change is the anchor, not the highest volume.
        assertEquals(listOf("v1", "resume", null, null), pick(vol(1, "reading", "2026-01-01T00:00:00Z", "2026-01-03T00:00:00Z", page = 2), done(2, "2026-01-02T00:00:00Z"), vol(3)))
        // Nothing to read: why, and the volume held back when there is one, whichever after the anchor first.
        assertEquals(listOf(null, null, "empty", null), pick())
        assertEquals(listOf(null, null, "caught_up", null), pick(done(1, "2026-01-01T00:00:00Z")))
        assertEquals(listOf(null, null, "earlier_unread", "v1"), pick(vol(1, "plan_to_read"), done(2, "2026-01-02T00:00:00Z"), vol(3, "dropped")))
        assertEquals(listOf("v2", "start", "held", null), pick(done(1, "2026-01-01T00:00:00Z"), vol(2, "on_hold")))
        assertEquals(listOf("v3", "resume", "held", null), pick(vol(1, "on_hold"), done(2, "2026-01-02T00:00:00Z"), vol(3, "on_hold", page = 5)))
    }

    /** An unsent change sorts after every server time, by queue place; the server's times by their instants. */
    @Test
    fun pendingChangesAreTheLatest() {
        // V1 was set to Reading offline; V2 was completed later on the server's clock: V1 is still the anchor.
        assertEquals(listOf("v1", "start", null, null), pick(vol(1, "reading", statusRank = 0), done(2, "2026-01-02T00:00:00Z"), vol(3)))
        // Two unsent changes: the later in the queue wins.
        assertEquals(listOf("v2", "resume", null, "v1"), pick(vol(1, "reading", readRank = 0, page = 1), vol(2, "reading", readRank = 1, page = 3), vol(3)))
        // A cleared volume has no time: the other is the anchor.
        assertEquals(listOf("v2", "resume", null, "v1"), pick(vol(1), vol(2, "reading", "2026-01-03T00:00:00Z", page = 2), vol(3)))
    }

    /** "Earlier unread" opens the earlier volume's page, which is what has to be on this device; any other no-target answer opens nothing. */
    @Test
    fun earlierUnreadOpensTheEarlierVolume() {
        assertEquals("v1", offlineContinue("s", listOf(vol(1, "plan_to_read"), done(2, "2026-01-02T00:00:00Z"), vol(3, "dropped"))).opens)
        assertEquals("v2", offlineContinue("s", listOf(done(1, "2026-01-01T00:00:00Z"), vol(2))).opens)
        assertEquals(null, offlineContinue("s", listOf(done(1, "2026-01-01T00:00:00Z"))).opens)
    }
}
