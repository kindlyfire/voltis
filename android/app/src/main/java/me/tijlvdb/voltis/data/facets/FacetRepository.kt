package me.tijlvdb.voltis.data.facets

import javax.inject.Inject
import javax.inject.Singleton
import me.tijlvdb.voltis.data.api.FacetEntry
import me.tijlvdb.voltis.data.api.FacetPage
import me.tijlvdb.voltis.data.api.VoltisApi

/** The Discover endpoints, uncached: Discover is online only. */
@Singleton
class FacetRepository @Inject constructor(private val api: VoltisApi) {
    /** A page of [kind]'s values; a blank [q] and a null [libraryId] are left out. */
    suspend fun list(kind: String, q: String, sort: String, order: String, libraryId: String?, offset: Int, limit: Int): FacetPage =
        api.facets(
            kind,
            buildMap {
                if (q.isNotEmpty()) put("q", q)
                put("sort", sort)
                put("order", order)
                if (libraryId != null) put("library_id", libraryId)
                put("offset", offset.toString())
                put("limit", limit.toString())
            },
        )

    /** Its count and roles are [libraryId]'s, or every library's. */
    suspend fun entry(kind: String, key: String, libraryId: String?): FacetEntry = api.facet(kind, key, libraryId)
}
