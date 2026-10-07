package me.tijlvdb.voltis.ui.discover

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.cancel
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withContext
import me.tijlvdb.voltis.data.reading.EngineTest
import me.tijlvdb.voltis.data.api.Facet
import me.tijlvdb.voltis.data.api.FacetPage
import me.tijlvdb.voltis.data.api.Library
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.api.testApi
import me.tijlvdb.voltis.data.auth.testStore
import me.tijlvdb.voltis.data.content.CatalogEvents
import me.tijlvdb.voltis.data.facets.FacetRepository
import me.tijlvdb.voltis.data.user.UserRepository
import me.tijlvdb.voltis.domain.catalog.FacetKind
import me.tijlvdb.voltis.domain.catalog.FacetSort
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

@OptIn(ExperimentalCoroutinesApi::class)
class DiscoverViewModelTest {
    @get:Rule
    val tmp = TemporaryFolder()

    /** Each list request waits for the test's answer, and gets it even when its caller was cancelled. */
    private class Api(real: VoltisApi) : VoltisApi by real {
        val asked = mutableListOf<Pair<Map<String, String>, CompletableDeferred<FacetPage>>>()

        override suspend fun libraries() = emptyList<Library>()

        override suspend fun facets(kind: String, params: Map<String, String>): FacetPage {
            val answer = CompletableDeferred<FacetPage>()
            asked += params + ("kind" to kind) to answer
            return withContext(NonCancellable) { answer.await() }
        }

        fun answer(index: Int, rows: Int, total: Int, keys: List<String> = List(rows) { "k$index-$it" }) =
            asked[index].second.complete(FacetPage(keys.map { Facet(it, it, 1) }, total))
    }

    @Test
    fun queriesPagesAndStaleAnswers() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val api = Api(testApi())
        val events = CatalogEvents()
        val users = UserRepository(api, testStore(tmp.newFolder().resolve("s.preferences_pb"), backgroundScope), events, backgroundScope)
        val vm = DiscoverViewModel(SavedStateHandle(), FacetRepository(api), users, EngineTest.FakeConnectivity { true }, events)
        try {
            fun asked(index: Int) = api.asked[index].first
            runCurrent()
            assertEquals(mapOf("kind" to "genres", "sort" to "count", "order" to "desc", "offset" to "0", "limit" to "100"), asked(0))
            api.answer(0, 100, 150)
            runCurrent()

            // Typing on within the debounce asks once, trimmed, for the last text.
            vm.setFilter("adv")
            advanceTimeBy(100)
            vm.setFilter(" adve ")
            advanceTimeBy(299)
            runCurrent()
            assertEquals(1, api.asked.size)
            advanceTimeBy(1)
            runCurrent()
            assertEquals("adve" to "0", asked(1)["q"] to asked(1)["offset"])
            // Starting over clears the rows.
            assertEquals(null, vm.rows)

            // Another kind while that is out: the filter carries, offset 0; the late answer changes nothing.
            vm.setKind(FacetKind.TAGS)
            runCurrent()
            assertEquals(mapOf("q" to "adve", "kind" to "tags", "offset" to "0"), asked(2).filterKeys { it in setOf("q", "kind", "offset") })
            api.answer(1, 100, 150)
            runCurrent()
            assertEquals(null, vm.rows)
            api.answer(2, 100, 230)
            runCurrent()
            assertEquals(100 to 230, vm.rows?.size to vm.total)

            // The next page one at a time. Two of its values moved across from the first page: they
            // aren't shown twice, and the next page still starts where the server's rows ended.
            vm.loadMore()
            vm.loadMore()
            runCurrent()
            assertEquals(4, api.asked.size)
            assertEquals("100" to "100", asked(3)["offset"] to asked(3)["limit"])
            api.answer(3, 100, 230, listOf("k2-98", "k2-99", "sort", "controls") + List(96) { "k3-$it" })
            runCurrent()
            assertEquals(198, vm.rows?.size)
            assertEquals(vm.rows?.size, vm.rows?.distinctBy { it.key }?.size)
            vm.loadMore()
            runCurrent()
            assertEquals("200", asked(4)["offset"])
            // A page of values all shown already: the next one is asked at once, with no scroll.
            val shownKeys = vm.rows!!.take(100).map { it.key }
            api.answer(4, 100, 330, shownKeys)
            runCurrent()
            assertEquals(198, vm.rows?.size)
            assertEquals("300", asked(5)["offset"])
            // More such pages with a growing total: five in a row, then it waits for Load more.
            for (index in 5..8) {
                api.answer(index, 100, 330 + (index - 4) * 100, shownKeys)
                runCurrent()
            }
            assertEquals(9, api.asked.size)
            assertEquals(false to true, vm.appending to vm.stalled)
            vm.loadMore()
            runCurrent()
            assertEquals("700" to false, asked(9)["offset"] to vm.stalled)
            // A short page is the end, though fewer values show than the total.
            api.answer(9, 20, 730)
            runCurrent()
            assertEquals(218, vm.rows?.size)
            assertEquals(false to false, vm.appending to vm.stalled)
            vm.loadMore()
            runCurrent()
            assertEquals(10, api.asked.size)
        } finally {
            vm.viewModelScope.cancel()
            Dispatchers.resetMain()
        }
    }

    @Test
    fun sortToggle() {
        // (sort, order, tapped) to the new sort and order.
        val cases = listOf(
            Triple("count", "desc", "count") to ("count" to "asc"),
            Triple("count", "asc", "count") to ("count" to "desc"),
            Triple("count", "asc", "name") to ("name" to "asc"),
            Triple("name", "asc", "name") to ("name" to "desc"),
            Triple("name", "desc", "count") to ("count" to "desc"),
        )
        for ((given, expected) in cases) assertEquals(given.toString(), expected, FacetSort.toggle(given.first, given.second, given.third))
    }
}
