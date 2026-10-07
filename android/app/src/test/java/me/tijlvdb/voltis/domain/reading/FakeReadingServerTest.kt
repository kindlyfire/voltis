package me.tijlvdb.voltis.domain.reading

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Test

/** The fake's rules that reading.go has and fakeReadingServer.ts doesn't; the engine tests rely on them. */
class FakeReadingServerTest {
    private fun body(vararg fields: Pair<String, Any>) =
        JsonObject(fields.associate { (k, v) -> k to if (v is Number) JsonPrimitive(v) else JsonPrimitive(v.toString()) })

    @Test
    fun serverRules() = runBlocking {
        val server = FakeReadingServer(last = 9)
        server.series("s", "v1", "v2")
        server.series("empty")
        server.invalid += "v2"
        for (id in listOf("s", "v1", "v2")) server.set(id) { copy(status = "reading", statusUpdatedAt = "earlier") }

        // Another device's clear of a series clears every volume, valid or not, under its revision.
        server.clear("s")
        val revision = server.stateOf("s").revision
        assertEquals(listOf(revision, revision), listOf("v1", "v2").map { server.stateOf(it).revision })
        assertEquals(listOf(null, null), listOf("v1", "v2").map { server.stateOf(it).status })

        // series_status needs a writer, and an unchanged status keeps its time.
        val noWriter = runCatching { server.post("v1", body("op" to "series_status", "status" to "on_hold")) }.exceptionOrNull()
        assertEquals("series_status needs writer_id and seq", (noWriter as ReadingFailure.Refused).message)
        server.set("s") { copy(status = "on_hold", statusUpdatedAt = "earlier") }
        server.post("v1", body("op" to "series_status", "status" to "on_hold", "writer_id" to "tqqqqqqqqqqqqqqqq", "seq" to 1))
        assertEquals("tqqqqqqqqqqqqqqqq:1" to "earlier", server.stateOf("s").let { it.revision to it.statusUpdatedAt })

        // A series without volumes is still a series: completing it gives no page.
        server.seriesReading("empty", body("action" to "mark_series_completed"))
        assertEquals("completed" to EmptyProgress, server.stateOf("empty").let { it.status to it.progress })
    }
}
