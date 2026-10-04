import { capitalize } from 'vue'
import type { LocationQueryRaw, RouteLocationRaw } from 'vue-router'
import type { FacetKind } from './api/types'

/** A genre or role slug as a label: `cover_artist` → "Cover artist". */
export const slugLabel = (slug: string) => capitalize(slug.replaceAll('_', ' '))

/** The value page of a server key, whose spaces become '-' in the URL. */
export const facetRoute = (
    kind: FacetKind,
    key: string,
    query?: LocationQueryRaw
): RouteLocationRaw => ({
    name: 'facet',
    params: { kind, key: key.replaceAll(' ', '-') },
    query,
})
