package me.tijlvdb.voltis.data.reading

import me.tijlvdb.voltis.data.content.CatalogChange.PositionSaved
import me.tijlvdb.voltis.data.content.CatalogChange.ReadingChanged
import org.junit.Assert.assertEquals
import org.junit.Test

/** readingSync.test.ts, `caches`: `invalidateReading` is [ReadingChanged], `markPositionSaved` is [PositionSaved]. */
class EngineCachesTest : EngineTest() {
    @Test
    fun `refetch after status changes at once, and after positions once the reader leaves`() = engineTest {
        server.series("s", "c")
        val (sync) = open("c")
        read(sync, 1)
        assertEquals(1, changes.count { it is ReadingChanged })
        read(sync, 2)
        assertEquals(1, changes.count { it is ReadingChanged })
        assertEquals(PositionSaved("c", "s"), changes.last { it is PositionSaved })
        sync.detach()
        settle()
        assertEquals(ReadingChanged("c", "s"), changes.last { it is ReadingChanged })
        assertEquals(2, changes.count { it is ReadingChanged })
    }

    @Test
    fun `the next reader opens where the last one’s exit write left the lane`() = engineTest {
        val (sync) = open("c")
        sync.moved(page(6))
        sync.detach()
        val next = open("c")
        assertEquals(page(6), next.sync.view.value.acked?.progress)
    }
}
