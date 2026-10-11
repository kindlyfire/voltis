package me.tijlvdb.voltis.data.content

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlin.math.abs
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.job
import kotlinx.coroutines.launch
import me.tijlvdb.voltis.data.api.Content
import me.tijlvdb.voltis.data.api.ContentBuckets
import me.tijlvdb.voltis.data.api.attemptResult

/**
 * A list as fixed pages of [PAGE_SIZE] rows, loaded around the visible range ([setRange]) like the web's
 * `useContentWindow`. An item's index is its offset in the list: a short page leaves a gap ([itemAt] null) and
 * shifts nothing. Answers from before the last [refresh] are dropped. All calls are on [scope]'s thread.
 */
class ContentWindow(
    private val scope: CoroutineScope,
    private val content: ContentRepository,
    private val params: ContentListParams,
    private val ready: suspend () -> Unit = {},
    start: Int = 0,
    private val onTotal: (Int) -> Unit = {},
) {
    /** A load failed; [retry] refreshes the whole window. Only one is pending at a time. */
    class Failure(val retry: () -> Unit)

    var buckets by mutableStateOf<ContentBuckets?>(null)
        private set

    /** The first load failed: there is no total to lay out. */
    var error by mutableStateOf<Throwable?>(null)
        private set

    var failure by mutableStateOf<Failure?>(null)
        private set

    var refreshing by mutableStateOf(false)
        private set

    private var pages by mutableStateOf(emptyMap<Int, List<Content>>())

    // The items to load around, the ones last applied, and the pages that is.
    private var wanted = start..start
    private var appliedItems = wanted
    private var applied = pageRange(appliedItems)
    private var paused = false
    private var timer: Job? = null
    private var pendingSince = 0L
    private var generation = 0
    private var bucketsPending = false
    private var bucketsFailed = false
    private var bucketsJob: Job? = null
    private val loading = HashMap<Int, Job>()
    private val failed = HashSet<Int>()

    /** Pages from before the last refresh that it has not replaced. */
    private val stale = HashSet<Int>()

    init {
        refresh()
    }

    fun itemAt(index: Int): Content? = pages[index / PAGE_SIZE]?.getOrNull(index % PAGE_SIZE)

    /** The items on screen with their overscan; their pages load once the range has rested. */
    fun setRange(first: Int, last: Int) {
        if (first..last == wanted) return
        wanted = first..last
        if (!paused) schedule()
    }

    /** A rail drag holds the range where it was; the end of it applies the latest at once. */
    fun pause(on: Boolean) {
        paused = on
        timer?.cancel()
        if (!on) apply()
    }

    /** Loads the buckets and the window again, in place: what is shown stays until replaced, also on a failure. */
    fun refresh() {
        generation++
        loading.values.forEach(Job::cancel)
        loading.clear()
        failed.clear()
        bucketsFailed = false
        failure = null
        error = null
        refreshing = true
        stale.clear()
        stale += pages.keys
        // The buckets publish on their own, as the web's do: the total and the rail don't wait for the pages.
        val gen = generation
        bucketsPending = true
        bucketsJob?.cancel()
        bucketsJob = scope.launch {
            ready()
            val answer = attemptResult { content.buckets(params) }
            if (gen != generation) return@launch
            bucketsPending = false
            answer.onSuccess {
                buckets = it
                onTotal(it.total)
                applied = pageRange(appliedItems)
            }.onFailure {
                bucketsFailed = true
                if (buckets == null) error = it else if (failure == null) failure = Failure(::refresh)
            }
            load()
        }
        load()
    }

    private fun schedule() {
        val now = System.currentTimeMillis()
        if (timer?.isActive != true) pendingSince = now
        timer?.cancel()
        // Debounced, but a range that keeps changing applies after MAX_WAIT_MS all the same.
        val wait = minOf(DEBOUNCE_MS, MAX_WAIT_MS - (now - pendingSince)).coerceAtLeast(0)
        timer = scope.launch {
            delay(wait)
            apply()
        }
    }

    private fun apply() {
        if (paused) return
        appliedItems = wanted
        applied = pageRange(appliedItems)
        load()
    }

    private fun load() {
        loading.keys.filter { it !in applied }.forEach { loading.remove(it)?.cancel() }
        // A page that left the window is tried again when it comes back.
        failed.retainAll(applied.toSet())
        if (failed.isEmpty() && !bucketsFailed) failure = null
        for (n in applied) {
            if ((n in pages && n !in stale) || n in loading || n in failed) continue
            val gen = generation
            loading[n] = scope.launch {
                val me = coroutineContext.job
                ready()
                val result = attemptResult { content.list(pageQuery(n)) }
                if (gen != generation) return@launch
                loading.remove(n, me)
                result.onSuccess {
                    pages = pages + (n to it.data)
                    stale -= n
                    evict()
                }.onFailure {
                    failed += n
                    if (failure == null) failure = Failure(::refresh)
                }
                settle()
            }
        }
        evict()
        settle()
    }

    /** A refresh is done once its buckets and the pages of the window as it is now have answered. */
    private fun settle() {
        if (!refreshing || bucketsPending || loading.isNotEmpty()) return
        pages = pages.filterKeys { it !in stale || it in applied }
        stale.clear()
        refreshing = false
    }

    /** Keeps the window's pages and, of the others, the nearest to it. */
    private fun evict() {
        if (pages.size <= MAX_PAGES) return
        val middle = (applied.first + applied.last) / 2
        val keep = pages.keys.sortedBy { if (it in applied) -1 else abs(it - middle) }.take(MAX_PAGES).toSet()
        pages = pages.filterKeys { it in keep }
    }

    private fun pageQuery(page: Int) = params.copy(limit = PAGE_SIZE, offset = page * PAGE_SIZE, count = false)

    /**
     * The pages one before to one after the items' own, within the list. Until the total is known,
     * only the page of the first item.
     */
    private fun pageRange(items: IntRange): IntRange {
        val total = buckets?.total ?: return (items.first / PAGE_SIZE).let { it..it }
        if (total == 0) return IntRange.EMPTY
        val last = (total - 1) / PAGE_SIZE
        return (items.first / PAGE_SIZE - 1).coerceIn(0, last)..(items.last / PAGE_SIZE + 1).coerceIn(0, last)
    }

    companion object {
        /** The web's: divisible by 1-6, 8, 10 and 12 columns. */
        const val PAGE_SIZE = 120
        const val MAX_PAGES = 10
        private const val DEBOUNCE_MS = 80L
        private const val MAX_WAIT_MS = 250L
    }
}
