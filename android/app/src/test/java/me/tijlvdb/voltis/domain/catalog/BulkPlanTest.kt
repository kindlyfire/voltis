package me.tijlvdb.voltis.domain.catalog

import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.ReadingStatus
import kotlinx.coroutines.delay
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class BulkPlanTest {
    private val items = listOf(ContentType.COMIC_SERIES, ContentType.BOOK_SERIES, ContentType.COMIC, ContentType.BOOK)
        .mapIndexed { i, type -> Selected(Content("c_$i", "Moss Harbor $i", type)) }
    private val series = items.take(2)
    private val singles = items.drop(2)

    /** P4 §6's table: only Completed of a series is its own command. */
    @Test
    fun statusTable() {
        val cases = listOf(
            null to items.map { BulkCommand.SetStatus(it, null) },
            ReadingStatus.READING to items.map { BulkCommand.SetStatus(it, ReadingStatus.READING) },
            ReadingStatus.ON_HOLD to items.map { BulkCommand.SetStatus(it, ReadingStatus.ON_HOLD) },
            ReadingStatus.DROPPED to items.map { BulkCommand.SetStatus(it, ReadingStatus.DROPPED) },
            ReadingStatus.PLAN_TO_READ to items.map { BulkCommand.SetStatus(it, ReadingStatus.PLAN_TO_READ) },
            ReadingStatus.COMPLETED to series.map { BulkCommand.CompleteSeries(it, true) } + singles.map { BulkCommand.SetStatus(it, ReadingStatus.COMPLETED) },
        )
        for ((status, expected) in cases) assertEquals("$status", expected, statusPlan(items, status, includeUnread = true))
        assertEquals(BulkCommand.CompleteSeries(series[0], false), statusPlan(series, ReadingStatus.COMPLETED, includeUnread = false)[0])
        assertEquals(items.map { BulkCommand.Clear(it) }, clearPlan(items))
    }

    private fun volume(id: String, parent: String, size: Long? = 100, type: String = ContentType.COMIC) =
        Content(id, "Vol $id", type, parentId = parent, fileSize = size)

    private fun pick(id: String, type: String) = Selected(Content(id, "Pick $id", type))

    /** P4 §6: volumes the store reports as present are left out, books are skipped, sizes add up. */
    @Test
    fun downloadTable() = runBlocking {
        val lists = mapOf(
            "s1" to listOf(volume("a", "s1"), volume("b", "s1"), volume("c", "s1", size = null), volume("note", "s1", type = ContentType.BOOK)),
            "s2" to listOf(volume("d", "s2"), volume("e", "s2"), volume("f", "s2")),
        )
        val present = setOf("b", "d", "e")
        val plan = downloadPlan(listOf(pick("s1", ContentType.COMIC_SERIES), pick("s2", ContentType.COMIC_SERIES), pick("bk", ContentType.BOOK), pick("bs", ContentType.BOOK_SERIES)), { present }) { lists.getValue(it) }
        assertEquals(listOf("a", "c", "f"), plan.offered.map { it.id })
        assertEquals(3, plan.already)
        assertEquals(2, plan.books)
        assertEquals(200L, plan.bytes) // a, f; c has no size
        assertTrue(plan.sizeUnknown)

        // A volume picked directly and also in a picked series counts once.
        val direct = downloadPlan(listOf(pick("a", ContentType.COMIC).copy(content = volume("a", "s1")), pick("s1", ContentType.COMIC_SERIES)), { setOf("c") }) { lists.getValue(it) }
        assertEquals(listOf("a", "b"), direct.offered.map { it.id })
        assertEquals(1, direct.already)
        assertEquals(false, direct.sizeUnknown)

        assertEquals(0, downloadPlan(listOf(pick("bs", ContentType.BOOK_SERIES)), { emptySet() }) { error("no list") }.count)
    }

    /** The limit is of what would be queued: 500 pass, 501 don't; and no more than four lists are asked at once. */
    @Test
    fun downloadLimitAndExpansion() = runTest {
        var open = 0
        var most = 0
        val series = (1..10).map { pick("s$it", ContentType.COMIC_SERIES) }
        val plan = downloadPlan(series, { emptySet() }) { id ->
            open++
            most = maxOf(most, open)
            delay(20)
            open--
            (1..50).map { volume("$id-$it", id) }
        }
        assertEquals(4, most)
        assertEquals(500, plan.count)
        assertEquals(false, plan.tooMany)
        val over = downloadPlan(series + pick("x", ContentType.COMIC), { emptySet() }) { id -> (1..50).map { volume("$id-$it", id) } }
        assertEquals(501, over.count)
        assertTrue(over.tooMany)
        // Left-out volumes don't count against it: 501 candidates, one already downloaded, is 500.
        val within = downloadPlan(series, { setOf("s1-1") }) { id -> (1..if (id == "s1") 51 else 50).map { volume("$id-$it", id) } }
        assertEquals(500, within.count)
        assertEquals(false, within.tooMany)
    }
}
