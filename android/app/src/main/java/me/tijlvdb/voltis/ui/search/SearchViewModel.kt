package me.tijlvdb.voltis.ui.search

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import javax.inject.Provider
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.attemptResult
import me.tijlvdb.voltis.data.content.ContentListParams
import me.tijlvdb.voltis.data.content.ContentRepository
import me.tijlvdb.voltis.data.downloads.DownloadRepository
import me.tijlvdb.voltis.domain.net.Connectivity
import me.tijlvdb.voltis.domain.net.settled

@HiltViewModel
class SearchViewModel @Inject constructor(
    private val content: ContentRepository,
    private val connectivity: Connectivity,
    // Only read by the grid's cards, so a test needn't build the repository.
    private val downloads: Provider<DownloadRepository>,
) : ViewModel() {
    /** The results' download marks, by content id. */
    val badges get() = downloads.get().badges

    private var term = ""

    /** The answer to the last term that got one, kept while the next loads; null without one. */
    var results by mutableStateOf<List<Content>?>(null)
        private set

    /** The term on screen has no answer yet. */
    var searching by mutableStateOf(false)
        private set

    var failed by mutableStateOf(false)
        private set

    private var job: Job? = null

    /** One request per term, once typing pauses. A newer term cancels the older one's request. */
    fun setTerm(text: String) {
        val trimmed = text.trim().take(MAX_TERM)
        if (trimmed == term) return
        term = trimmed
        failed = false
        if (trimmed.isEmpty()) {
            job?.cancel()
            results = null
            searching = false
        } else {
            search(DEBOUNCE_MS)
        }
    }

    fun retry() = search(0)

    private fun search(delayMs: Long) {
        job?.cancel()
        searching = true
        job = viewModelScope.launch {
            delay(delayMs)
            connectivity.settled()
            failed = false
            // The server orders by relevance when a search is set.
            val params = ContentListParams(search = term, parentId = ContentListParams.TOP_LEVEL, limit = LIMIT, count = false)
            val found = attemptResult { content.list(params).data }.getOrNull()
            // An answer that still arrives for a superseded term changes nothing.
            ensureActive()
            results = found
            failed = found == null
            searching = false
        }
    }

    private companion object {
        const val DEBOUNCE_MS = 300L
        const val MAX_TERM = 200
        const val LIMIT = 30
    }
}
