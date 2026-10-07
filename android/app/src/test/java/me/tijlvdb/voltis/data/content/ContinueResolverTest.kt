package me.tijlvdb.voltis.data.content

import java.io.IOException
import java.time.Instant
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ContinueEntry
import me.tijlvdb.voltis.data.reading.EngineTest
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.reading.Recency
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import retrofit2.HttpException
import retrofit2.Response

class ContinueResolverTest {
    @Test
    fun resolves() = runTest {
        fun entry(id: String, type: String) = ContinueEntry(Content(id, "Item $id", type), isNew = false)
        val comicLanes = listOf(
            ContinueCandidate("old", ContentType.COMIC, Recency.Server(Instant.parse("2026-10-01T10:00:00Z"))),
            // Later, though it sorts first as a string.
            ContinueCandidate("new", ContentType.COMIC, Recency.Server(Instant.parse("2026-10-01T10:00:00.5Z"))),
        )
        val bookLane = listOf(ContinueCandidate("b", ContentType.BOOK, Recency.Server(Instant.parse("2026-10-01T10:00:00Z"))))
        val unreachable = IOException("unreachable")
        val genuine = HttpException(Response.error<Unit>(500, """{"error": "Broken"}""".toResponseBody("application/json".toMediaType())))
        // Online, the server's answer or failure; the lanes; what opens.
        val cases = listOf(
            Triple(true, listOf(entry("c", ContentType.COMIC)), comicLanes) to ContinueOpen.Reader("c"),
            Triple(true, listOf(entry("b", ContentType.BOOK)), comicLanes) to ContinueOpen.Page("b"),
            Triple(true, emptyList<ContinueEntry>(), comicLanes) to null,
            Triple(true, unreachable, comicLanes) to ContinueOpen.Reader("new"),
            // Voltis answered: its view wins over the lanes.
            Triple(true, genuine, comicLanes) to null,
            Triple(false, listOf(entry("c", ContentType.COMIC)), comicLanes) to ContinueOpen.Reader("new"),
            Triple(false, unreachable, bookLane) to ContinueOpen.Page("b"),
            Triple(false, unreachable, emptyList<ContinueCandidate>()) to ContinueOpen.NothingOffline,
        )
        for ((case, expected) in cases) {
            val (online, answer, lanes) = case
            val connectivity = EngineTest.FakeConnectivity { online }.apply { this.online.value = online }
            @Suppress("UNCHECKED_CAST")
            val resolver = ContinueResolver(connectivity, { (answer as? Exception)?.let { throw it } ?: answer as List<ContinueEntry> }, { lanes })
            assertEquals(case.toString(), expected, resolver.resolve())
        }
    }

    /** A damaged or closed database leaves the current screen: no crash, no target. */
    @Test
    fun aFailingLocalReadResolvesToNothing() = runTest {
        val offline = EngineTest.FakeConnectivity { false }.apply { online.value = false }
        assertNull(ContinueResolver(offline, { emptyList() }, { error("damaged page") }).resolve())
        assertNull(ContinueResolver(offline, { emptyList() }, { null }).resolve())
    }

    @Test
    fun waitsForTheStartsCheck() = runTest {
        val fake = EngineTest.FakeConnectivity { false }
        val checking = MutableStateFlow(true)
        val connectivity = object : Connectivity by fake {
            override val checking = checking
        }
        var requests = 0
        val lanes = listOf(ContinueCandidate("l", ContentType.COMIC, Recency.Server(Instant.parse("2026-10-01T10:00:00Z"))))
        val resolver = ContinueResolver(connectivity, { requests++; emptyList() }, { lanes })
        val result = async { resolver.resolve() }
        runCurrent()
        // Still checking: online reads true, but nothing is asked.
        assertEquals(0, requests)
        assertEquals(false, result.isCompleted)
        // The check found the server away: the lane, with no request.
        fake.online.value = false
        checking.value = false
        assertEquals(ContinueOpen.Reader("l"), result.await())
        assertEquals(0, requests)
    }
}
