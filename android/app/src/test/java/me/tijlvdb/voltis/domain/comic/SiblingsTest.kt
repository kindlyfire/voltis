package me.tijlvdb.voltis.domain.comic

import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

/** The cases of `pages/read/useSiblings.test.ts` that ReaderViewModelTest doesn't already cover. */
class SiblingsTest {
    private val data = FakeReaderData()

    @Test
    fun standaloneIsReadyWithoutAsking() = runTest {
        assertEquals(Siblings(Siblings.Status.Ready), loadSiblings(data, "c_1", parentId = null))
        assertEquals(emptyList<String>(), data.calls)
    }

    @Test
    fun aListWithoutTheVolumeIsAnErrorUntilARetryFollowsItToItsNewSeries() = runTest {
        for (ids in listOf(emptyList(), listOf("c_8", "c_9"))) {
            data.lists["s_1"] = ids
            assertEquals(Siblings(Siblings.Status.Error), loadSiblings(data, "c_1", "s_1"))
        }

        data.comics["c_1"] = volume("c_1", "s_2")
        data.lists["s_2"] = listOf("c_0", "c_1", "c_2")
        val retried = loadSiblings(data, "c_1", "s_1", reread = true)
        assertEquals(Siblings.Status.Ready, retried.status)
        assertEquals(1, retried.index)
        assertEquals("c_2", retried.next?.id)
    }
}
