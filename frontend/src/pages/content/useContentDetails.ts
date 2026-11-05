import { computed, type MaybeRefOrGetter, toValue } from 'vue'
import type { RouteLocationRaw } from 'vue-router'
import type { MetadataLinkRef } from '@/utils/api/metadata'
import type { Content, FacetKind } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'
import { facetRoute, slugLabel } from '@/utils/facets'
import { lengthSummary, readingSpeed } from '@/utils/readingTime'

export type DetailItem = { text: string; to?: RouteLocationRaw; suffix?: string }
export type DetailRow = {
    key: 'staff' | 'publishers' | 'published' | 'length' | 'genres' | 'tags' | 'links'
    label: string
    value?: string
    items?: DetailItem[]
    links?: MetadataLinkRef[]
}

/** The id of the tabs section on the content page, to scroll to. */
export const CONTENT_TABS_ID = 'content-tabs'

const FIELD_KINDS = {
    staff: 'people',
    genres: 'genres',
    tags: 'tags',
    publishers: 'publishers',
} as const satisfies Record<keyof NonNullable<Content['facet_keys']>, FacetKind>

/** A full date in words; a year or year-month stays as is (a day would be made up). */
function formatDate(value: string) {
    if (/^\d{4}(-\d{2})?$/.test(value)) return value
    const date = new Date(value)
    if (isNaN(date.getTime())) return value
    // Date-only values parse as UTC midnight: format in UTC so the day doesn't shift.
    return date.toLocaleDateString(undefined, {
        dateStyle: 'long',
        timeZone: 'UTC',
    })
}

/** The metadata of a content as labelled rows, in display order. */
export function useContentDetails(content: MaybeRefOrGetter<Content>) {
    const qMe = usersApi.useMe()
    return computed(() => {
        const c = toValue(content)
        const m = c.meta ?? {}
        const keys = c.facet_keys
        // Links only where the server gave a key: valid top-level entries.
        const items = (field: keyof typeof FIELD_KINDS, texts: string[]): DetailItem[] =>
            texts.map((text, i) => {
                const key = keys?.[field][i]
                return { text, to: key ? facetRoute(FIELD_KINDS[field], key) : undefined }
            })
        const rows: DetailRow[] = []
        if (m.staff?.length) {
            const staff = m.staff
            rows.push({
                key: 'staff',
                label: 'Staff',
                items: items(
                    'staff',
                    staff.map(s => s.name)
                ).map((item, i) => ({ ...item, suffix: ` (${staff[i]!.role})` })),
            })
        }
        if (m.publishers?.length) {
            rows.push({
                key: 'publishers',
                label: 'Publishers',
                items: items('publishers', m.publishers),
            })
        }
        if (m.publication_date) {
            rows.push({
                key: 'published',
                label: 'Published',
                value: formatDate(m.publication_date),
            })
        }
        if (c.length) {
            rows.push({
                key: 'length',
                label: 'Length',
                value: lengthSummary(c.length, readingSpeed(qMe.data.value?.preferences)),
            })
        }
        if (m.genres?.length) {
            rows.push({
                key: 'genres',
                label: 'Genres',
                items: items('genres', m.genres.map(slugLabel)),
            })
        }
        if (m.tags?.length) rows.push({ key: 'tags', label: 'Tags', items: items('tags', m.tags) })
        if (m.links?.length) rows.push({ key: 'links', label: 'Links', links: m.links })
        return rows
    })
}
