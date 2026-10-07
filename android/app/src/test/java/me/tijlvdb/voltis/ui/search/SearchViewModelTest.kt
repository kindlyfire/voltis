package me.tijlvdb.voltis.ui.search

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
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentPage
import me.tijlvdb.voltis.data.api.ForAccount
import me.tijlvdb.voltis.data.api.VoltisApi
import me.tijlvdb.voltis.data.api.testApi
import me.tijlvdb.voltis.data.content.ContentRepository
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class SearchViewModelTest {
    /** Answers a search when the test says so, and even to a caller that has been cancelled. */
    private class Api(real: VoltisApi) : VoltisApi by real {
        val asked = mutableListOf<String>()
        private val answers = mutableMapOf<String, CompletableDeferred<ContentPage>>()

        override suspend fun content(params: Map<String, String>, account: ForAccount?): ContentPage {
            val term = params.getValue("search")
            asked += term
            return withContext(NonCancellable) { answers.getOrPut(term) { CompletableDeferred() }.await() }
        }

        fun answer(term: String) = answers.getValue(term).complete(ContentPage(listOf(Content(id = term, title = term, type = "comic"))))
    }

    @Test
    fun onlyTheCurrentTermAnswers() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        val api = Api(testApi())
        val vm = SearchViewModel(ContentRepository(api), EngineTest.FakeConnectivity { true }) { error("no downloads") }
        fun shown() = vm.results?.map { it.id } to vm.searching
        fun type(term: String, wait: Long) {
            vm.setTerm(term)
            advanceTimeBy(wait)
            runCurrent()
        }

        // Typing on within the debounce asks once, for the last term.
        type("tin", 100)
        type("tin l", 299)
        assertEquals(emptyList<String>(), api.asked)
        type(" tin l ", 1)
        assertEquals(listOf("tin l"), api.asked)

        // A new term while that request is out: its late answer changes nothing.
        type("tin lantern", 300)
        api.answer("tin l")
        runCurrent()
        assertEquals(null to true, shown())
        api.answer("tin lantern")
        runCurrent()
        assertEquals(listOf("tin lantern") to false, shown())

        // Cleared during a request: no results, not searching, and the answer doesn't bring them back.
        type("tin lanterns", 300)
        assertEquals(listOf("tin lantern") to true, shown())
        type("", 0)
        assertEquals(null to false, shown())
        api.answer("tin lanterns")
        runCurrent()
        assertEquals(null to false, shown())
        assertEquals(listOf("tin l", "tin lantern", "tin lanterns"), api.asked)

        vm.viewModelScope.cancel()
    }

    // Also after a failure: a Main left set fails the next class's first test.
    @After
    fun tearDown() = Dispatchers.resetMain()
}
