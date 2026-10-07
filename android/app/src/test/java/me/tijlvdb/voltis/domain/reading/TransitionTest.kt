package me.tijlvdb.voltis.domain.reading

import java.io.File
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import me.tijlvdb.voltis.data.api.AppJson
import me.tijlvdb.voltis.data.api.ContentType
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** The table TestReadingTransition (backend/routes/reading_test.go) runs too. When it fails, reading.go changed: port the change to Transition.kt. */
class TransitionTest {
    private fun JsonElement?.string() = (this as? JsonPrimitive)?.takeIf { it !is JsonNull }?.content

    /** `last_read` labels stand for times; the strings are as good as any. */
    private fun snapshot(json: JsonObject) =
        Snapshot(json["status"].string(), json["progress"]!!.jsonObject, json["last_read"].string())

    @Test
    fun sharedTable() {
        val cases = AppJson.parseToJsonElement(File("../../backend/routes/testdata/reading_transitions.json").readText()).jsonArray
        assertTrue(cases.isNotEmpty())
        for (case in cases.map { it.jsonObject }) {
            val name = case["name"].string()
            val op = case["op"]!!.jsonObject
            val readingOp = ReadingOp(
                op["op"].string()!!, op["status"].string(), op["progress"] as? JsonObject,
                (op["snapshot"] as? JsonObject)?.let(::snapshot),
            )
            val cur = snapshot(case["cur"]!!.jsonObject)
            val got = transition(cur, readingOp, case["end"]!!.jsonObject, "now")
            val want = case["want"]!!.jsonObject
            assertEquals(name, snapshot(want), got.next)
            assertEquals(name, want["outcome"].string(), got.outcome)
            assertEquals(name, want["write"]!!.jsonPrimitive.boolean, got.write)
            assertEquals(name, want["previous"]!!.jsonPrimitive.boolean, got.previous != null)
            got.previous?.let { assertEquals(name, cur.status, it.status) }
        }
    }

    @Test
    fun seriesStartsAndEnds() {
        for ((op, outcome, starts) in listOf(
            Triple(ReadingOp("position"), Outcome.STARTED, true),
            Triple(ReadingOp("position"), Outcome.MOVED_TO_READING, true),
            Triple(ReadingOp("position"), Outcome.SAVED, false),
            Triple(ReadingOp("finish"), Outcome.COMPLETED, true),
            Triple(ReadingOp("set_status", "reading"), Outcome.STATUS_SET, true),
            Triple(ReadingOp("set_status", "on_hold"), Outcome.STATUS_SET, false),
            Triple(ReadingOp("clear"), Outcome.CLEARED, false),
            Triple(ReadingOp("restore"), Outcome.RESTORED, false),
        )) {
            assertEquals("${op.op} $outcome", starts, startsSeries(op, outcome))
        }
        assertEquals(
            AppJson.parseToJsonElement("""{"current_page": 39, "progress_percent": 100, "at_end": true}"""),
            endProgress(ContentType.COMIC, 40),
        )
        // A comic without pages ends on page 0; a series has no progress.
        assertEquals(JsonPrimitive(0), endProgress(ContentType.COMIC, 0)["current_page"])
        assertEquals(EmptyProgress, endProgress(ContentType.COMIC_SERIES, 40))
    }
}
