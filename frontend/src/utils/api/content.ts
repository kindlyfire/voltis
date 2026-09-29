import { keepPreviousData, useMutation, useQuery, type Query } from '@tanstack/vue-query'
import { promiseTimeout } from '@vueuse/core'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { API_URL, apiFetch } from '../fetch'
import { queryClient } from '../misc'
import { isEnabled, type QueryOptions } from './_utils'
import { invalidateMatching, isContentWindow, libraryScope } from './catalog'
import type {
    BookStructure,
    BrokenRefsFixRequest,
    BrokenRefsSummaryItem,
    BrokenUserToContent,
    CountResponse,
    Content,
    ContentBuckets,
    ContentListParams,
    Cover,
    DownloadInfo,
    LibraryUrisResponse,
    OrphanedMetadata,
    OrphanTarget,
    OrphansFixRequest,
    OrphansSummaryItem,
    Paginated,
    ReadingStatus,
    RecentlyReadEntry,
    UserToContent,
    UserToContentUpdate,
} from './types'

export function coverUrl(c: Cover): string | null {
    return c.cover_version ? `${API_URL}/files/cover/${c.id}?v=${c.cover_version}` : null
}

export interface PageParams {
    search?: string
    limit?: number
    offset?: number
}

function pageQuery(p: PageParams): string {
    const searchParams = new URLSearchParams()
    if (p.search) searchParams.append('search', p.search)
    if (p.limit !== undefined) searchParams.append('limit', String(p.limit))
    if (p.offset !== undefined) searchParams.append('offset', String(p.offset))
    const query = searchParams.toString()
    return query ? `?${query}` : ''
}

/** Only the recently-read query: the readers keep sibling lists active under ['content', 'list']. */
const recentlyReadFilters = {
    queryKey: ['content', 'list'],
    predicate: (q: Query) => q.queryKey[3] === 'recently-read',
}

export async function invalidateRecentlyRead() {
    // Without data, invalidating joins an in-flight fetch, whose response may predate the write.
    await queryClient.cancelQueries(recentlyReadFilters)
    await queryClient.invalidateQueries(recentlyReadFilters)
}

// A reader's exit write, which the recently-read fetch waits for so it can't return the old order.
let pendingRecentlyRead: Promise<unknown> = Promise.resolve()

export function trackRecentlyReadWrite(p?: Promise<unknown>) {
    if (!p) return
    // Chained: a second reader's write mustn't replace the first.
    pendingRecentlyRead = Promise.allSettled([pendingRecentlyRead, p]).then(() => {})
}

/** Moves `item`'s entry to the front so HomePage mounts in the new order; the refetch adds missing ones. */
export function bumpRecentlyRead(item: Content) {
    queryClient.setQueriesData<RecentlyReadEntry[]>(recentlyReadFilters, entries => {
        if (!entries) return entries
        const i = entries.findIndex(e =>
            e.series ? e.series.id === item.parent_id : e.item.id === item.id
        )
        return i > 0 ? [entries[i], ...entries.toSpliced(i, 1)] : entries
    })
}

/** A status write can change its series' status too (backend propagation), and a reader's last
 * write can land after the grid it returns to has fetched. */
export async function invalidateStatusChange(parentId: string | null) {
    const keys = [['content', 'list'], ...(parentId ? [['content', parentId]] : [])]
    await Promise.all(keys.map(queryKey => queryClient.cancelQueries({ queryKey })))
    await Promise.all(keys.map(queryKey => queryClient.invalidateQueries({ queryKey })))
}

export const invalidateContentWindow = () => invalidateMatching(queryClient, isContentWindow)

/** Items per page of the content window. Divisible by 1–6, 8, 10 and 12 columns. */
export const PAGE_SIZE = 120

export const contentWindowKey = (p: ContentListParams) =>
    ['content', 'list', libraryScope(p.library_id), 'window', p] as const

export function listSearchParams(p: ContentListParams): URLSearchParams {
    const searchParams = new URLSearchParams()
    if (p.parent_id) searchParams.append('parent_id', p.parent_id)
    if (p.library_id) searchParams.append('library_id', p.library_id)
    for (const t of p.type ?? []) searchParams.append('type', t)
    if (p.valid !== undefined) searchParams.append('valid', String(p.valid))
    if (p.reading_status) searchParams.append('reading_status', p.reading_status)
    if (p.starred !== undefined) searchParams.append('starred', String(p.starred))
    if (p.has_status !== undefined) searchParams.append('has_status', String(p.has_status))
    if (p.has_rating !== undefined) searchParams.append('has_rating', String(p.has_rating))
    if (p.search) searchParams.append('search', p.search)
    if (p.limit !== undefined) searchParams.append('limit', String(p.limit))
    if (p.offset !== undefined) searchParams.append('offset', String(p.offset))
    if (p.sort) searchParams.append('sort', p.sort)
    if (p.sort_order) searchParams.append('sort_order', p.sort_order)
    return searchParams
}

export const contentApi = {
    useGet: (
        id: MaybeRefOrGetter<string | undefined | null>,
        options: QueryOptions<Content> = {}
    ) =>
        useQuery({
            queryKey: ['content', id],
            queryFn: async () => contentApi.get(toValue(id)!),
            enabled: isEnabled(id),
            ...options,
        }),

    get: async (id: string, init?: RequestInit) => {
        return apiFetch<Content>(`/content/${id}`, init)
    },

    useList: (
        params: MaybeRefOrGetter<ContentListParams | undefined> = {},
        options: QueryOptions<Paginated<Content>> = {}
    ) =>
        useQuery({
            queryKey: ['content', 'list', libraryScope(() => toValue(params)?.library_id), params],
            queryFn: async () => {
                const query = listSearchParams(toValue(params)!).toString()
                return apiFetch<Paginated<Content>>(`/content${query ? `?${query}` : ''}`)
            },
            enabled: isEnabled(params),
            ...options,
        }),

    useBuckets: (params: MaybeRefOrGetter<ContentListParams>) =>
        useQuery({
            queryKey: computed(() => [...contentWindowKey(toValue(params)), 'buckets'] as const),
            queryFn: async ({ queryKey }) =>
                apiFetch<ContentBuckets>(`/content/buckets?${listSearchParams(queryKey[4])}`),
        }),

    /** Page `page` of the window, without the total, which comes from the buckets. */
    listPage: async (params: ContentListParams, page: number) => {
        const q = listSearchParams({ ...params, limit: PAGE_SIZE, offset: page * PAGE_SIZE })
        q.set('count', 'false')
        return apiFetch<{ data: Content[] }>(`/content?${q}`)
    },

    /** The ids at `offset`..`offset + limit - 1` of the list, in its order. */
    ids: async (params: ContentListParams, offset: number, limit: number) => {
        const q = listSearchParams({ ...params, offset, limit })
        return apiFetch<{ ids: string[] }>(`/content/ids?${q}`)
    },

    /** Returns how many items changed, once the content queries have refetched. */
    bulkUserData: async (
        body: { ids: string[] } & (
            | { action: 'set_status'; status: ReadingStatus | null }
            | { action: 'reset' }
        )
    ) => {
        const res = await apiFetch<CountResponse>('/content/bulk/user-data', {
            method: 'POST',
            body: JSON.stringify(body),
        })
        await invalidateMatching(queryClient, key => key[0] === 'content')
        return res
    },

    useRecentlyRead: (limit = 10) =>
        useQuery({
            // Under ['content', 'list'], so the list invalidations cover it too.
            queryKey: ['content', 'list', libraryScope(undefined), 'recently-read', limit],
            queryFn: async ({ signal }) => {
                // apiFetch has no timeout, so a hung write mustn't hold the row back forever.
                await Promise.race([pendingRecentlyRead, promiseTimeout(3000)])
                return apiFetch<RecentlyReadEntry[]>(`/content/recently-read?limit=${limit}`, {
                    signal,
                })
            },
        }),

    useDownloadInfo: (id: MaybeRefOrGetter<string | undefined | null>) =>
        useQuery({
            queryKey: ['content', 'download-info', id],
            queryFn: async () => apiFetch<DownloadInfo>(`/files/download-info/${toValue(id)}`),
            enabled: isEnabled(id),
        }),

    useBookStructure: (id: MaybeRefOrGetter<string | undefined | null>) =>
        useQuery({
            queryKey: ['content', 'book-structure', id],
            queryFn: async () => contentApi.bookStructure(toValue(id)!),
            enabled: isEnabled(id),
        }),

    bookStructure: async (id: string, init?: RequestInit) =>
        apiFetch<BookStructure>(`/files/book-chapters/${id}`, init),

    /** `version` (`fileVersion`) makes the response cacheable. */
    bookDocument: async (id: string, href: string, version: string | null, init?: RequestInit) => {
        const params = new URLSearchParams({ href })
        if (version) params.set('v', version)
        const res = await fetch(`${API_URL}/files/book-chapter/${id}?${params}`, {
            credentials: 'include',
            ...init,
        })
        if (!res.ok) throw new Error(`Failed to fetch ${href}`)
        return res.text()
    },

    useLists: (id: MaybeRefOrGetter<string | undefined | null>) =>
        useQuery({
            queryKey: ['content', 'lists', id],
            queryFn: async () => contentApi.lists(toValue(id)!),
            enabled: isEnabled(id),
        }),

    lists: async (contentId: string) => {
        return apiFetch<string[]>(`/content/${contentId}/lists`)
    },

    useUpdateUserData: () =>
        useMutation({
            mutationFn: (data: UserToContentUpdate & { contentId: string }) =>
                contentApi.updateUserData(data.contentId, data),
            // Returned, so `mutateAsync` resolves once the fresh data is in.
            onSuccess: (_data, variables) =>
                Promise.all([
                    queryClient.invalidateQueries({ queryKey: ['content', variables.contentId] }),
                    invalidateMatching(
                        queryClient,
                        key => key[0] === 'content' && key[1] === 'list'
                    ),
                ]),
        }),

    updateUserData: async (
        contentId: string,
        data: UserToContentUpdate,
        init?: RequestInit
    ): Promise<UserToContent> => {
        return apiFetch<UserToContent>(`/content/${contentId}/user-data`, {
            method: 'POST',
            body: JSON.stringify(data),
            ...init,
        })
    },

    setSeriesItemStatuses: async (
        contentId: string,
        status: ReadingStatus | null,
        untilId?: string
    ): Promise<void> => {
        await apiFetch(`/content/${contentId}/series-item-statuses`, {
            method: 'POST',
            body: JSON.stringify({ status, until_id: untilId }),
        })
    },

    listLibraryUris: async (libraryId: string): Promise<LibraryUrisResponse> => {
        return apiFetch<LibraryUrisResponse>(`/content/refs/${libraryId}`)
    },

    useLibraryUris: (libraryId: MaybeRefOrGetter<string | undefined | null>) =>
        useQuery({
            queryKey: ['content', 'library-uris', libraryScope(libraryId)],
            queryFn: async () => contentApi.listLibraryUris(toValue(libraryId)!),
            enabled: isEnabled(libraryId),
        }),

    useBrokenRefsSummary: (options: QueryOptions<BrokenRefsSummaryItem[]> = {}) =>
        useQuery({
            queryKey: ['content', 'broken-refs-summary'],
            queryFn: async () => apiFetch<BrokenRefsSummaryItem[]>('/content/broken-refs'),
            ...options,
        }),

    useBrokenRefs: (
        libraryId: MaybeRefOrGetter<string | undefined | null>,
        params: MaybeRefOrGetter<PageParams> = {},
        options: QueryOptions<Paginated<BrokenUserToContent>> = {}
    ) =>
        useQuery({
            queryKey: ['content', 'broken-refs', libraryScope(libraryId), params],
            queryFn: async () =>
                apiFetch<Paginated<BrokenUserToContent>>(
                    `/content/broken-refs/${toValue(libraryId)}${pageQuery(toValue(params))}`
                ),
            enabled: isEnabled(libraryId),
            ...options,
        }),

    fixBrokenRefs: async (libraryId: string, body: BrokenRefsFixRequest): Promise<void> => {
        await apiFetch(`/content/broken-refs/${libraryId}`, {
            method: 'POST',
            body: JSON.stringify(body),
        })
    },

    useOrphansSummary: (options: QueryOptions<OrphansSummaryItem[]> = {}) =>
        useQuery({
            queryKey: ['content', 'orphaned-metadata-summary'],
            queryFn: async () => apiFetch<OrphansSummaryItem[]>('/content/orphaned-metadata'),
            ...options,
        }),

    useOrphans: (
        libraryId: MaybeRefOrGetter<string | undefined | null>,
        params: MaybeRefOrGetter<PageParams> = {}
    ) =>
        useQuery({
            queryKey: ['content', 'orphaned-metadata', libraryScope(libraryId), params],
            queryFn: async () =>
                apiFetch<Paginated<OrphanedMetadata>>(
                    `/content/orphaned-metadata/${toValue(libraryId)}${pageQuery(toValue(params))}`
                ),
            enabled: isEnabled(libraryId),
        }),

    /** Content that orphans can move to: series only for orphans with links. */
    useOrphanTargets: (
        libraryId: MaybeRefOrGetter<string>,
        params: MaybeRefOrGetter<{ search: string; series: boolean }>
    ) =>
        useQuery({
            queryKey: ['content', 'orphaned-metadata', libraryScope(libraryId), 'targets', params],
            queryFn: async () => {
                const { search, series } = toValue(params)
                const query = new URLSearchParams({
                    q: search,
                    series: String(series),
                    limit: '50',
                })
                return apiFetch<{ data: OrphanTarget[] }>(
                    `/content/orphaned-metadata/${toValue(libraryId)}/targets?${query}`
                )
            },
            placeholderData: keepPreviousData,
        }),

    fixOrphans: async (libraryId: string, body: OrphansFixRequest): Promise<void> => {
        await apiFetch(`/content/orphaned-metadata/${libraryId}`, {
            method: 'POST',
            body: JSON.stringify(body),
        })
    },
}
