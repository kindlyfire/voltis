package me.tijlvdb.voltis.data.content

import kotlinx.serialization.Serializable
import me.tijlvdb.voltis.data.api.ReadingStatus

/** A grid's filters: the web's URL query of `ContentGrid.vue`. */
@Serializable
data class GridFilters(
    val starred: Boolean = false,
    /** [YES], [NO], one of [ReadingStatus], or null. */
    val status: String? = null,
    /** [YES], [NO] or null. */
    val rating: String? = null,
    val sort: String = ContentSort.TITLE,
    val sortOrder: String = ASC,
) {
    /** Whether anything but the star differs from its default: the star is its own toggle. */
    val hasFilters get() = copy(starred = false) != GridFilters()

    /** The continue sorts list volumes to read, each shown with its series: cards with a subtitle line. */
    val listsContinue get() = sort == ContentSort.CONTINUE || sort == ContentSort.RECENTLY_UPDATED

    /** Choosing a sort sets its default order. */
    fun withSort(sort: String) = copy(sort = sort, sortOrder = defaultOrder(sort))

    fun reset() = GridFilters(starred = starred)

    /**
     * The query for these filters over [base], as `queryParams` in `ContentGrid.vue`. A base that
     * fixes the sort (a series' contents) keeps it, and takes only the order.
     */
    fun toParams(base: ContentListParams): ContentListParams = base.copy(
        starred = true.takeIf { starred },
        hasStatus = yesNo(status),
        readingStatus = status?.takeIf { it in ReadingStatus.ALL },
        hasRating = yesNo(rating),
        sort = base.sort ?: sort,
        sortOrder = sortOrder,
    )

    companion object {
        const val YES = "yes"
        const val NO = "no"
        const val ASC = "asc"
        const val DESC = "desc"

        fun defaultOrder(sort: String) = if (sort == ContentSort.TITLE) ASC else DESC

        private fun yesNo(value: String?) = when (value) {
            YES -> true
            NO -> false
            else -> null
        }
    }
}
