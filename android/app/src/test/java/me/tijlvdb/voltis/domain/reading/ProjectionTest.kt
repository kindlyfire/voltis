package me.tijlvdb.voltis.domain.reading

import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.ContentType
import org.junit.Assert.assertEquals
import org.junit.Test

/** What unsent ops show (P2 §5, Projection), with the expected values written out rather than taken from `transition`. */
class ProjectionTest {
    private fun page(n: Int) = JsonObject(mapOf("current_page" to JsonPrimitive(n)))

    private fun json(vararg fields: Pair<String, Any?>) = JsonObject(
        fields.associate { (key, value) ->
            key to when (value) {
                null -> JsonNull
                is JsonElement -> value
                is Boolean -> JsonPrimitive(value)
                else -> JsonPrimitive(value.toString())
            }
        },
    )

    /** The end of a ten-page comic, and of a volume whose page count isn't known. */
    private val end = JsonObject(mapOf("current_page" to JsonPrimitive(9), "progress_percent" to JsonPrimitive(100), "at_end" to JsonPrimitive(true)))
    private val endUnknown = JsonObject(mapOf("progress_percent" to JsonPrimitive(100), "at_end" to JsonPrimitive(true)))

    private fun volume(id: String, status: String? = null, progress: JsonObject = EmptyProgress, lastReadAt: String? = null, pages: Int? = 10) =
        Lane(id, parentId = "s", type = ContentType.COMIC, pageCount = pages, state = ReadingState("srv:1", status, progress = progress, lastReadAt = lastReadAt))

    private fun series(status: String? = null) = Lane("s", type = ContentType.COMIC_SERIES, state = ReadingState("srv:2", status))

    private fun lanes(vararg lanes: Lane) = lanes.associateBy { it.contentId }

    private fun op(lane: String, kind: String, payload: JsonObject, createdAt: Long = 0, guard: JsonObject? = null) =
        Op(contentId = lane, kind = kind, payload = payload, createdAt = createdAt, guard = guard)

    private fun command(lane: String, name: String, vararg fields: Pair<String, Any?>) = op(lane, OpKind.COMMAND, json("op" to name, *fields))

    private fun seriesOp(action: String, vararg volumes: String) =
        op("s", OpKind.SERIES, json("series_id" to "s", "action" to action), guard = json("series" to null, "volumes" to json(*volumes.map { it to null }.toTypedArray())))

    @Test
    fun anItemsOwnOpsInOrder() {
        val held = volume("a", "on_hold", page(1), "2026-01-01T00:00:00Z")
        val moved = op("a", OpKind.POSITION, json("current_page" to JsonPrimitive(3), "at_end" to true), createdAt = 1000)
        val all = lanes(held, series("on_hold"))
        // A position moves a held item to Reading, at its own time, and never ends it.
        assertEquals(mapOf("a" to Snapshot("reading", page(3), "1970-01-01T00:00:01Z")), project(listOf(moved), all).filterKeys { it == "a" })
        // A status set after it keeps the position; a finish ends at the last page.
        assertEquals(Snapshot("dropped", page(3), "1970-01-01T00:00:01Z"), project(listOf(moved, command("a", "set_status", "status" to "dropped")), all)["a"])
        val finish = op("a", OpKind.FINISH, end, createdAt = 2000)
        assertEquals(Snapshot("completed", end, "1970-01-01T00:00:02Z"), project(listOf(moved, finish), all)["a"])
        assertEquals(Snapshot("completed", end, "2026-01-01T00:00:00Z"), project(listOf(command("a", "mark_completed")), all)["a"])
        assertEquals(Snapshot(null, page(1), "2026-01-01T00:00:00Z"), project(listOf(command("a", "set_status", "status" to null)), all)["a"])
        assertEquals(Snapshot(null, EmptyProgress, null), project(listOf(moved, command("a", "clear")), all)["a"])
        // An undo puts back what it carries, and the series' status with it.
        val undo = op(
            "a", OpKind.UNDO,
            json("snapshot" to json("status" to "on_hold", "progress" to page(1), "last_read_at" to null), "series" to json("revision" to "t1:1", "status" to "completed", "status_updated_at" to null)),
        )
        assertEquals(mapOf("a" to Snapshot("on_hold", page(1), null), "s" to Snapshot("completed", EmptyProgress, null)), project(listOf(moved, undo), all))
        // Nothing unsent, nothing shown: the lanes show what was acknowledged.
        assertEquals(emptyMap<String, Snapshot>(), project(emptyList(), all))
    }

    @Test
    fun aVolumeStartsItsSeries() {
        val position = op("a", OpKind.POSITION, page(3))
        val finish = op("a", OpKind.FINISH, end)
        fun shownSeries(status: String?, vararg ops: Op) = project(ops.toList(), lanes(volume("a"), series(status)))["s"]?.status
        assertEquals("reading", shownSeries(null, position))
        assertEquals("reading", shownSeries("plan_to_read", command("a", "set_status", "status" to "reading")))
        // A series it doesn't start isn't shown: the reader's copy of it may be newer than its lane.
        assertEquals(null, shownSeries("on_hold", position))
        // Starting a volume reopens a completed series; finishing one doesn't.
        assertEquals("reading", shownSeries("completed", position))
        assertEquals(null, shownSeries("completed", finish))
        assertEquals("reading", shownSeries(null, command("a", "mark_completed")))
        // A status that isn't Reading starts nothing; the reader's series commands write the series itself.
        assertEquals(null, shownSeries(null, command("a", "set_status", "status" to "on_hold")))
        assertEquals("dropped", shownSeries("reading", command("a", "series_status", "status" to "dropped")))
        assertEquals("completed", shownSeries("reading", command("a", "series_status", "series" to json("revision" to "t1:1", "status" to "completed"))))
    }

    @Test
    fun seriesOpsShowOnTheVolumesTheyCover() {
        val a = volume("a", "reading", page(4), "2026-01-01T00:00:00Z")
        val b = volume("b", pages = null)
        val c = volume("c", "on_hold", page(2))
        val through = seriesOp("mark_through", "a", "b")
        val covered = mapOf("a" to Snapshot("completed", end, "2026-01-01T00:00:00Z"), "b" to Snapshot("completed", endUnknown, null))
        // The series is started, never completed; a volume after the last one is left alone.
        assertEquals(covered + ("s" to Snapshot("reading", EmptyProgress, null)), project(listOf(through), lanes(a, b, c, series())))
        assertEquals(covered, project(listOf(through), lanes(a, b, c, series("on_hold"))))
        // Covering nothing, it starts nothing.
        assertEquals(emptyMap<String, Snapshot>(), project(listOf(seriesOp("mark_through")), lanes(a, b, c, series())))

        val completed = seriesOp("mark_series_completed", "a", "b")
        assertEquals(covered + ("s" to Snapshot("completed", EmptyProgress, null)), project(listOf(completed), lanes(a, b, c, series("reading"))))

        val blank = Snapshot(null, EmptyProgress, null)
        val clear = command("s", "clear").copy(guard = json("series" to null, "volumes" to json("a" to null, "b" to null, "c" to null)))
        assertEquals(mapOf("s" to blank, "a" to blank, "b" to blank, "c" to blank), project(listOf(clear), lanes(a, b, c, series("reading"))))
        // A volume without a lane is skipped; reading queued after the op shows on top of it, and starts the cleared series.
        val after = op("b", OpKind.POSITION, page(1), createdAt = 3000)
        assertEquals(
            mapOf("s" to Snapshot("reading", EmptyProgress, null), "b" to Snapshot("reading", page(1), "1970-01-01T00:00:03Z")),
            project(listOf(clear, after), lanes(b, series("reading"))),
        )
    }

    /** Pending changes sort after every server time, by queue place; clear removes the stamp, restore puts the server's time back. */
    @Test
    fun pendingChangesAreStampedByQueuePlace() {
        val t1 = "2026-01-01T00:00:00Z"
        val t2 = "2026-01-02T00:00:00Z"
        val a = volume("a", "reading", page(1), t1).let { it.copy(state = it.state.copy(statusUpdatedAt = t2)) }
        val read = op("a", OpKind.POSITION, page(3), createdAt = 1000)
        val snapshot = json("status" to "reading", "progress" to page(1), "last_read_at" to t1)
        fun stamps(vararg ops: Op) = projected(ops.toList(), lanes(a, series("reading"))).stamps["a"]
        assertEquals(
            listOf(
                // A position: after every server time. Its status doesn't change, so that stamp stays the server's.
                Stamps(null, 0, t2, null),
                // A status away and back is a real change both times: it keeps a rank.
                Stamps(t1, null, null, 1),
                // A cleared item has no times; a read after it ranks again, and starts it.
                Stamps(null, null, null, null),
                Stamps(null, 1, null, 1),
                // An undo puts back the snapshot's server time, and drops the rank; its status didn't change.
                Stamps(t1, null, t2, null),
            ),
            listOf(
                stamps(read),
                stamps(command("a", "set_status", "status" to "on_hold"), command("a", "set_status", "status" to "reading")),
                stamps(read, command("a", "clear")),
                stamps(command("a", "clear"), read),
                stamps(read, op("a", OpKind.UNDO, json("snapshot" to snapshot, "series" to null))),
            ),
        )
    }
}
