package me.tijlvdb.voltis.data.reading

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentType
import me.tijlvdb.voltis.data.api.FileData
import me.tijlvdb.voltis.data.api.UserData
import me.tijlvdb.voltis.data.downloads.forSeed
import me.tijlvdb.voltis.domain.reading.EmptyProgress
import me.tijlvdb.voltis.domain.reading.ReadingState
import org.junit.Assert.assertEquals
import org.junit.Test

/** Not in the web: `ReadingSync.seed` (P2 §5, Seeding). */
class EngineSeedTest : EngineTest() {
    /** A row whose state is the server's [seq]th: that of the revision's number unless given. */
    private fun row(id: String, rev: String?, n: Int?, status: String? = "reading", pages: Int? = null, seq: Long? = null) = Content(
        id, "Title $id", ContentType.COMIC, parentId = "s", libraryId = "l",
        fileData = FileData(pages?.let { count -> List(count) { JsonArray(listOf(JsonPrimitive("$it.png"))) } }),
        userData = rev?.let { UserData(status = status, progress = n?.let(::page), revision = it, readingSeq = seq ?: it.substringAfter(':').toLong()) },
    )

    @Test
    fun `makes a lane, a null user_data being the blank state, and keeps a known page count`() = engineTest {
        engine.seed(listOf(row("c", "srv:1", 4, pages = 10), row("d", null, null)))
        val c = store.lane("c")!!
        assertEquals(ReadingState("srv:1", "reading", progress = page(4), seq = 1), c.state)
        assertEquals(listOf(true, page(4), "Title c", "s", "l", 10), listOf(c.acked, c.here, c.title, c.parentId, c.libraryId, c.pageCount))
        val d = store.lane("d")!!
        assertEquals(listOf(true, ReadingState(), EmptyProgress), listOf(d.acked, d.state, d.here))

        // A list row, without pages, leaves the count as it was.
        engine.seed(listOf(row("c", "srv:1", 4)))
        assertEquals(10, store.lane("c")?.pageCount)

        // A download's seed of the detail row, held over its copy's publication: the copy's count stays.
        val held = listOf(row("c", "srv:1", 4, pages = 9).forSeed())
        engine.pageCount("c", 12)
        engine.seed(held)
        assertEquals(12, store.lane("c")?.pageCount)
    }

    @Test
    fun `overwrites a lane that retains nothing, setting here, and counts a foreign revision`() = engineTest {
        engine.seed(listOf(row("c", "srv:1", 4)))
        engine.seed(listOf(row("c", "srv:2", 30)))
        val c = store.lane("c")!!
        assertEquals(listOf("srv:2", page(30), page(30), 1), listOf(c.state.revision, c.state.progress, c.here, c.foreignEpoch))
        // Our own write isn't someone else's.
        engine.seed(listOf(row("c", "${writer.writerId}:7", 31, seq = 50)))
        assertEquals(page(31) to 1, store.lane("c")!!.let { it.here to it.foreignEpoch })
    }

    @Test
    fun `leaves a lane with unsent reading, or a reader, as it is`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        val (sync) = open("c")
        engine.seed(listOf(row("c", "srv:9", 30)))
        assertEquals(page(2), store.lane("c")?.state?.progress)

        server.unreachable = true
        read(sync, 5)
        sync.detach()
        settle()
        engine.seed(listOf(row("c", "srv:9", 30)))
        assertEquals(page(2) to page(5), store.lane("c")!!.let { it.state.progress to it.here })
    }

    @Test
    fun `ignores a seed whose state is older than the lane's base`() = engineTest {
        server.set("c") { copy(status = "reading", progress = page(2)) }
        open("c").sync.detach()
        settle()
        engine.seed(listOf(row("c", "srv:0", 1)))
        assertEquals(page(2), store.lane("c")?.state?.progress)
        engine.seed(listOf(row("c", "srv:9", 30)))
        assertEquals(page(30), store.lane("c")?.state?.progress)
    }

    @Test
    fun `keeps the newer state when an older seed comes after a newer one or a series answer`() = engineTest {
        engine.seed(listOf(row("c", "srv:30", 30)))
        engine.seed(listOf(row("c", "srv:12", 12)))
        assertEquals(page(30) to page(30), store.lane("c")!!.let { it.state.progress to it.here })
        // The same state again, and an older one, change nothing; a later one applies.
        engine.seed(listOf(row("e", "srv:30", 30)))
        engine.seed(listOf(row("e", "srv:30", 30)))
        engine.seed(listOf(row("e", "srv:12", 12)))
        assertEquals(page(30) to page(30), store.lane("e")!!.let { it.state.progress to it.here })
        engine.seed(listOf(row("e", "srv:40", 40)))
        assertEquals(page(40) to page(40), store.lane("e")!!.let { it.state.progress to it.here })

        // A reading answer carries the series as it is now: an older seed of the series doesn't undo it.
        server.series("s", "v")
        engine.seed(listOf(Content("s", "s", ContentType.COMIC_SERIES)))
        server.set("s") { copy(status = "reading") }
        open("v").sync.detach()
        settle()
        val adopted = server.stateOf("s").revision
        assertEquals(adopted, store.lane("s")?.state?.revision)
        engine.seed(listOf(Content("s", "s", ContentType.COMIC_SERIES)))
        assertEquals(adopted to "reading", store.lane("s")!!.state.let { it.revision to it.status })
    }
}
