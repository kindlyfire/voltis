package me.tijlvdb.voltis.domain.catalog

/** The kinds of Discover values, as the API's paths spell them. */
object FacetKind {
    const val GENRES = "genres"
    const val TAGS = "tags"
    const val PEOPLE = "people"
    const val PUBLISHERS = "publishers"

    val ALL = listOf(GENRES, TAGS, PEOPLE, PUBLISHERS)
}

/** A Discover value's page. */
data class FacetRef(val kind: String, val key: String)

object FacetSort {
    const val COUNT = "count"
    const val NAME = "name"
    const val ASC = "asc"
    const val DESC = "desc"

    fun defaultOrder(sort: String) = if (sort == NAME) ASC else DESC

    /** The sort and order after a tap on [tapped]: the active one flips, the other starts at its default. */
    fun toggle(sort: String, order: String, tapped: String): Pair<String, String> = when (tapped) {
        sort -> sort to if (order == ASC) DESC else ASC
        else -> tapped to defaultOrder(tapped)
    }
}

/** A value's name as shown: a genre's is its slug. */
fun facetLabel(kind: String, name: String) = if (kind == FacetKind.GENRES) slugLabel(name) else name
