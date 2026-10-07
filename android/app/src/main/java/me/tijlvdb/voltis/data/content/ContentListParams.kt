package me.tijlvdb.voltis.data.content

/** A `GET /content` query. */
data class ContentListParams(
    val parentId: String? = null,
    val libraryId: String? = null,
    val valid: Boolean? = null,
    val readingStatus: String? = null,
    val starred: Boolean? = null,
    val hasStatus: Boolean? = null,
    val hasRating: Boolean? = null,
    val search: String? = null,
    val limit: Int? = null,
    val offset: Int? = null,
    val sort: String? = null,
    val sortOrder: String? = null,
    /** False leaves out the total, which is then null. */
    val count: Boolean? = null,
    /** One of `FacetKind`; with [facet], the items that have that value. */
    val facetKind: String? = null,
    val facet: String? = null,
    /** A person's role, with [facet] only. */
    val facetRole: String? = null,
) {
    /** Only the fields that are set, as `listSearchParams` in `utils/api/content.ts`. */
    fun toQuery(): Map<String, String> = buildMap {
        fun param(name: String, value: Any?) {
            if (value != null && value != "") put(name, value.toString())
        }
        param("parent_id", parentId)
        param("library_id", libraryId)
        param("valid", valid)
        param("reading_status", readingStatus)
        param("starred", starred)
        param("has_status", hasStatus)
        param("has_rating", hasRating)
        param("search", search)
        param("limit", limit)
        param("offset", offset)
        param("sort", sort)
        param("sort_order", sortOrder)
        param("count", count)
        param("facet_kind", facetKind)
        param("facet", facet)
        param("facet_role", facetRole)
    }

    companion object {
        /** The `parentId` that asks for top-level entries. */
        const val TOP_LEVEL = "null"

        /**
         * A series' volumes in its order, all of them (no limit). `asc` must be sent (the default is
         * `desc`): the reading guards rely on this order matching the server's `seriesReading`.
         */
        fun volumes(seriesId: String) =
            ContentListParams(parentId = seriesId, sort = ContentSort.ORDER, sortOrder = GridFilters.ASC, count = false)
    }
}

/** The `sort` values this app asks for. */
object ContentSort {
    const val TITLE = "title"
    const val CREATED_AT = "created_at"
    const val LAST_READ_AT = "last_read_at"
    const val RATING = "rating"
    const val USER_RATING = "user_rating"
    const val RELEASE_DATE = "release_date"
    const val UNREAD_COUNT = "unread_children_count"
    const val CONTINUE = "continue"
    const val RECENTLY_UPDATED = "recently_updated"
    const val HISTORY = "history"

    /** A series' own order. */
    const val ORDER = "order"
}
