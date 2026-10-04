import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { apiFetch } from '../fetch'
import { libraryScope } from './catalog'
import type { Facet, FacetEntry, FacetKind, FacetListParams, Paginated } from './types'

// Unscoped even with a library: names and existence are global, so any catalog change refetches.
export const facetsApi = {
    useList: (kind: MaybeRefOrGetter<FacetKind>, params: MaybeRefOrGetter<FacetListParams>) =>
        useQuery({
            queryKey: ['content', 'facets', libraryScope(null), kind, 'list', params],
            queryFn: ({ signal }) => {
                const { q, sort, order, library_id, limit, offset } = toValue(params)
                const query = new URLSearchParams({ limit: String(limit), offset: String(offset) })
                if (q) query.set('q', q)
                if (sort) query.set('sort', sort)
                if (order) query.set('order', order)
                if (library_id) query.set('library_id', library_id)
                return apiFetch<Paginated<Facet>>(`/facets/${toValue(kind)}?${query}`, { signal })
            },
            placeholderData: keepPreviousData,
        }),

    /** Getters, so a page reused across /tags/a → /tags/b follows the route. */
    useEntry: (
        kind: MaybeRefOrGetter<FacetKind>,
        key: MaybeRefOrGetter<string>,
        libraryId: MaybeRefOrGetter<string | null>
    ) =>
        useQuery({
            queryKey: ['content', 'facets', libraryScope(null), kind, 'entry', key, libraryId],
            queryFn: ({ signal }) => {
                const lib = toValue(libraryId)
                const query = lib ? `?${new URLSearchParams({ library_id: lib })}` : ''
                // The key is raw user input until the server folds it.
                const path = `/facets/${toValue(kind)}/${encodeURIComponent(toValue(key))}`
                return apiFetch<FacetEntry>(path + query, { signal })
            },
            placeholderData: keepPreviousData,
        }),
}
