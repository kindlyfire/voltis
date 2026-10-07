package me.tijlvdb.voltis.domain.downloads

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** The rule of automatic downloads (P2 §17), table-driven: v1..v6 are comics in the series' order. */
class AutoPolicyTest {
    private val finished = AutoRow(DownloadState.DONE, hasCopy = true, keep = false, pinned = false)

    /** A volume with one status as both the projection and the acknowledged state. */
    private fun vol(
        id: String,
        status: String? = null,
        type: String = "comic",
        valid: Boolean = true,
        position: Int? = id.removePrefix("v").toInt(),
        row: AutoRow? = null,
        acked: String? = status,
        held: Boolean = false,
    ) = AutoVolume(id, type, valid, position, status, acked, held, row)

    private fun input(
        volumes: List<AutoVolume>,
        keepNext: Int = 2,
        deleteFinished: Boolean = false,
        offered: Set<String> = emptySet(),
        ordered: Boolean = volumes.filter { it.valid }.all { it.position != null },
        unsent: Boolean = false,
        readingHeld: Boolean = false,
    ) = AutoInput(SeriesPolicy("s", keepNext, deleteFinished), volumes, ordered, unsent, readingHeld, offered)

    private val six = (1..6).map { vol("v$it") }

    private fun withStatus(vararg changes: Pair<String, String>) = six.map { v -> changes.toMap()[v.id]?.let { v.copy(shown = it, acked = it) } ?: v }

    private fun plan(input: AutoInput) = autoPlan(input)

    @Test
    fun theWindowFollowsTheAnchor() {
        val cases = listOf(
            // Name to (volumes, keepNext) to queued.
            "nothing read: the first N" to (six to 2) to listOf("v1", "v2"),
            "the volume in progress counts" to (withStatus("v1" to "completed", "v2" to "reading") to 2) to listOf("v2", "v3"),
            "a finished volume is passed" to (withStatus("v1" to "completed") to 2) to listOf("v2", "v3"),
            "an unread volume before the anchor is left out" to (withStatus("v3" to "reading") to 2) to listOf("v3", "v4"),
            "dropped volumes are skipped" to (withStatus("v1" to "completed", "v2" to "dropped") to 2) to listOf("v3", "v4"),
            "books are skipped and not counted" to (six.map { if (it.id == "v1" || it.id == "v3") it.copy(type = "book") else it } to 2) to listOf("v2", "v4"),
            "an invalid finished volume is neither anchor nor window" to
                (listOf(vol("v1"), vol("v2"), vol("x", "completed", valid = false, position = null), vol("v3")) to 2) to listOf("v1", "v2"),
            "the end of the list" to (withStatus("v5" to "completed") to 3) to listOf("v6"),
            "the order of the list is the order of the window" to (six.reversed() to 2) to listOf("v6", "v5"),
        )
        for ((case, queued) in cases) {
            val (name, args) = case
            val (volumes, n) = args
            val plan = plan(input(volumes, keepNext = n))
            assertEquals(name, queued, plan.queue)
            assertEquals(name, queued.toSet(), plan.offer)
            assertTrue(name, plan.delete.isEmpty())
        }
    }

    @Test
    fun queueAndOfferAreTheDelta() {
        val downloaded = finished.copy(state = DownloadState.FAILED, hasCopy = false)
        // A row in any state isn't queued, but is offered; an offered volume isn't queued or offered again.
        val volumes = six.map { if (it.id == "v1") it.copy(row = downloaded) else it }
        assertEquals(AutoPlan(listOf("v2"), setOf("v1", "v2"), emptyList()), plan(input(volumes)))
        assertEquals(AutoPlan(listOf("v2"), setOf("v2"), emptyList()), plan(input(volumes, offered = setOf("v1"))))
        // The steady state: nothing to do. A volume deleted by hand stays offered, and the window doesn't reach for another.
        assertTrue(plan(input(six, offered = setOf("v1", "v2"))).isEmpty)
        assertEquals(AutoPlan(listOf("v3"), setOf("v3"), emptyList()), plan(input(withStatus("v1" to "completed"), offered = setOf("v1", "v2"))))
        // Reordered by the server: the window moves over the new order, and the offers stay.
        assertEquals(AutoPlan(listOf("v3"), setOf("v3"), emptyList()), plan(input(listOf(six[1], six[2], six[0]) + six.drop(3), offered = setOf("v1", "v2"))))
        assertEquals(AutoPlan(emptyList(), emptySet(), emptyList()), plan(input(six, keepNext = 0, offered = emptySet())))
    }

    @Test
    fun anUnknownOrderQueuesNothingButStillDeletes() {
        val volumes = listOf(
            vol("v1", "completed", row = finished),
            vol("v2", position = null),
            vol("v3"),
            vol("gone", "completed", valid = false, position = null, row = finished),
        )
        val unordered = input(volumes, deleteFinished = true)
        assertEquals(false, unordered.ordered)
        // The invalid volume is deleted too, and is never in the window.
        assertEquals(AutoPlan(emptyList(), emptySet(), listOf("v1", "gone")), plan(unordered))
    }

    @Test
    fun eachDeleteConditionBlocksOnItsOwn() {
        fun deleted(row: AutoRow? = finished, status: String? = "completed", acked: String? = status, held: Boolean = false, readingHeld: Boolean = false, unsent: Boolean = false, on: Boolean = true) =
            plan(input(listOf(vol("v1", status, acked = acked, held = held, row = row)), keepNext = 0, deleteFinished = on, readingHeld = readingHeld, unsent = unsent)).delete
        assertEquals(listOf("v1"), deleted())
        assertTrue("not done", deleted(finished.copy(state = DownloadState.QUEUED)).isEmpty())
        assertTrue("a replacement waits", deleted(finished.copy(state = DownloadState.RUNNING)).isEmpty())
        assertTrue("no copy", deleted(finished.copy(hasCopy = false)).isEmpty())
        assertTrue("no row", deleted(null).isEmpty())
        assertTrue("not acknowledged", deleted(acked = "reading").isEmpty())
        assertTrue("not completed", deleted(status = "reading").isEmpty())
        assertTrue("held", deleted(held = true).isEmpty())
        assertTrue("keep", deleted(finished.copy(keep = true)).isEmpty())
        assertTrue("pinned", deleted(finished.copy(pinned = true)).isEmpty())
        assertTrue("the engine holds an unstored mutation", deleted(readingHeld = true).isEmpty())
        assertTrue("unsent", deleted(unsent = true).isEmpty())
        assertTrue("off", deleted(on = false).isEmpty())
    }
}
