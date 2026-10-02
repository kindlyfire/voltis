import { useMutation, useQuery } from '@tanstack/vue-query'
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
    Paginated,
    ReadingStatus,
    UncountedPage,
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
    if (p.count !== undefined) searchParams.append('count', String(p.count))
    return searchParams
}

const listQuery = <T>(params: MaybeRefOrGetter<ContentListParams | undefined>) => ({
    queryKey: ['content', 'list', libraryScope(() => toValue(params)?.library_id), params],
    queryFn: async () => {
        const query = listSearchParams(toValue(params)!).toString()
        return apiFetch<T>(`/content${query ? `?${query}` : ''}`)
    },
    enabled: isEnabled(params),
})

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

    /** `pageSizes` computes missing comic page sizes, which can take a while on first open. */
    get: async (id: string, init?: RequestInit, opts?: { pageSizes?: boolean }) => {
        return apiFetch<Content>(`/content/${id}${opts?.pageSizes ? '?page_sizes=1' : ''}`, init)
    },

    /** Counted; see useListUncounted for `count: false`. */
    useList: (
        params: MaybeRefOrGetter<(ContentListParams & { count?: true }) | undefined> = {},
        options: QueryOptions<Paginated<Content>> = {}
    ) => useQuery({ ...listQuery<Paginated<Content>>(params), ...options }),

    /** useList without the count query. */
    useListUncounted: (params: MaybeRefOrGetter<ContentListParams | undefined>) =>
        useQuery(
            listQuery<UncountedPage<Content>>(() => {
                const p = toValue(params)
                return p && { ...p, count: false }
            })
        ),

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
            | { action: 'set_status'; status: ReadingStatus | null; include_children?: boolean }
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

    /** The user's row at `uri`, which repairing a broken ref onto it would merge with. */
    userDataAt: async (libraryId: string, uri: string) =>
        apiFetch<BrokenUserToContent | null>(
            `/content/refs/${libraryId}/user-data?${new URLSearchParams({ uri })}`
        ),

    fixBrokenRefs: async (libraryId: string, body: BrokenRefsFixRequest): Promise<void> => {
        await apiFetch(`/content/broken-refs/${libraryId}`, {
            method: 'POST',
            body: JSON.stringify(body),
        })
    },
}
