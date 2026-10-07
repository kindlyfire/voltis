package me.tijlvdb.voltis.data.content

import me.tijlvdb.voltis.domain.catalog.FacetKind
import me.tijlvdb.voltis.ui.grid.FacetScope
import me.tijlvdb.voltis.ui.grid.GridSource
import me.tijlvdb.voltis.ui.grid.base
import org.junit.Assert.assertEquals
import org.junit.Test

class ContentListParamsTest {
    /** The filter combinations of `queryParams` in `ContentGrid.vue`, as the query that is sent. */
    @Test
    fun filtersBecomeTheQuery() {
        val library = ContentListParams(libraryId = "lib_1", parentId = "null")
        val top = mapOf("library_id" to "lib_1", "parent_id" to "null")
        data class Case(val name: String, val filters: GridFilters, val base: ContentListParams, val expected: Map<String, String>)
        val cases = listOf(
            Case("defaults", GridFilters(), library, top + mapOf("sort" to "title", "sort_order" to "asc")),
            Case(
                "the star, a reading status and no rating",
                GridFilters(starred = true, status = "on_hold", rating = "no"),
                library,
                top + mapOf("starred" to "true", "reading_status" to "on_hold", "has_rating" to "false", "sort" to "title", "sort_order" to "asc"),
            ),
            Case(
                "any status and any rating",
                GridFilters(status = "yes", rating = "yes"),
                library,
                top + mapOf("has_status" to "true", "has_rating" to "true", "sort" to "title", "sort_order" to "asc"),
            ),
            Case(
                "no status, an unknown rating value, another sort",
                GridFilters(status = "no", rating = "5").withSort("created_at"),
                library,
                top + mapOf("has_status" to "false", "sort" to "created_at", "sort_order" to "desc"),
            ),
            Case(
                "a continue sort over every library",
                GridFilters(sort = "continue", sortOrder = "desc"),
                ContentListParams(),
                mapOf("sort" to "continue", "sort_order" to "desc"),
            ),
            Case(
                "a base that fixes the sort takes only the order",
                GridFilters(sort = "rating", sortOrder = "desc"),
                ContentListParams(parentId = "c_1", sort = "order"),
                mapOf("parent_id" to "c_1", "sort" to "order", "sort_order" to "desc"),
            ),
        )
        for (case in cases) assertEquals(case.name, case.expected, case.filters.toParams(case.base).toQuery())

        // A Discover value's grid: the top level, its value, and a role only for a person.
        val genre = GridSource.Facet(FacetKind.GENRES, "science fiction")
        val person = GridSource.Facet(FacetKind.PEOPLE, "mireille dunmore")
        val facet = mapOf("parent_id" to "null", "sort" to "title", "sort_order" to "asc")
        data class FacetCase(val source: GridSource, val scope: FacetScope, val expected: Map<String, String>)
        val facetCases = listOf(
            FacetCase(genre, FacetScope(), facet + mapOf("facet_kind" to "genres", "facet" to "science fiction")),
            FacetCase(genre, FacetScope("lib_1", "writer"), facet + mapOf("facet_kind" to "genres", "facet" to "science fiction", "library_id" to "lib_1")),
            FacetCase(person, FacetScope(role = "writer"), facet + mapOf("facet_kind" to "people", "facet" to "mireille dunmore", "facet_role" to "writer")),
        )
        for ((source, scope, expected) in facetCases) assertEquals(expected, GridFilters().toParams(source.base(GridFilters(), scope)).toQuery())

        // The dot: the star alone isn't a filter; Reset keeps it.
        assertEquals(false, GridFilters(starred = true).hasFilters)
        assertEquals(true, GridFilters(sortOrder = "desc").hasFilters)
        assertEquals(GridFilters(starred = true), GridFilters(starred = true, status = "yes", sort = "rating").reset())
    }
}
