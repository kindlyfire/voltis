import { useMutation, useQuery, type UseQueryOptions } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter, type UnwrapRef } from 'vue'
import { API_URL, apiFetch } from '../fetch'
import type {
    BookChapter,
    BrokenRefsFixRequest,
    BrokenRefsSummaryItem,
    BrokenUserToContent,
    Content,
    ContentListParams,
    ContentMetadata,
    DownloadInfo,
    LibraryUrisResponse,
    MetadataLayersResponse,
    Paginated,
    ReadingStatus,
    UserToContent,
    UserToContentUpdate,
} from './types'
import { isEnabled } from './_utils'
import { queryClient } from '../misc'

type QueryOptions<T> = Omit<UnwrapRef<UseQueryOptions<T>>, 'queryKey' | 'queryFn'>

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

    get: async (id: string) => {
        return apiFetch<Content>(`/content/${id}`)
    },

    useList: (
        params: MaybeRefOrGetter<ContentListParams | undefined> = {},
        options: QueryOptions<Paginated<Content>> = {}
    ) =>
        useQuery({
            queryKey: ['content', 'list', params],
            queryFn: async () => {
                const p = toValue(params)!
                const searchParams = new URLSearchParams()
                if (p.parent_id) searchParams.append('parent_id', p.parent_id)
                if (p.library_id) searchParams.append('library_id', p.library_id)
                if (p.type) {
                    for (const t of p.type) {
                        searchParams.append('type', t)
                    }
                }
                if (p.valid !== undefined) searchParams.append('valid', String(p.valid))
                if (p.reading_status) searchParams.append('reading_status', p.reading_status)
                if (p.starred !== undefined) searchParams.append('starred', String(p.starred))
                if (p.search) searchParams.append('search', p.search)
                if (p.limit !== undefined) searchParams.append('limit', String(p.limit))
                if (p.offset !== undefined) searchParams.append('offset', String(p.offset))
                if (p.sort) searchParams.append('sort', p.sort)
                if (p.sort_order) searchParams.append('sort_order', p.sort_order)

                const query = searchParams.toString()
                return apiFetch<Paginated<Content>>(`/content${query ? `?${query}` : ''}`)
            },
            enabled: isEnabled(params),
            ...options,
        }),

    useDownloadInfo: (id: MaybeRefOrGetter<string | undefined | null>) =>
        useQuery({
            queryKey: ['content', 'download-info', id],
            queryFn: async () => apiFetch<DownloadInfo>(`/files/download-info/${toValue(id)}`),
            enabled: isEnabled(id),
        }),

    useBookChapters: (id: MaybeRefOrGetter<string | undefined | null>) =>
        useQuery({
            queryKey: ['content', 'book-chapters', id],
            queryFn: async () => apiFetch<BookChapter[]>(`/files/book-chapters/${toValue(id)}`),
            enabled: isEnabled(id),
        }),

    useBookChapter: (
        id: MaybeRefOrGetter<string | undefined | null>,
        href: MaybeRefOrGetter<string | undefined | null>
    ) =>
        useQuery({
            queryKey: ['content', 'book-chapter', id, href],
            queryFn: async () => {
                const params = new URLSearchParams({ href: toValue(href)! })
                const res = await fetch(`${API_URL}/files/book-chapter/${toValue(id)}?${params}`, {
                    credentials: 'include',
                })
                if (!res.ok) throw new Error('Failed to fetch chapter')
                return res.text()
            },
            enabled: isEnabled([id, href]),
        }),

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
            onSuccess(_data, variables) {
                queryClient.invalidateQueries({ queryKey: ['content', variables.contentId] })
                queryClient.invalidateQueries({
                    queryKey: ['content', 'list'],
                })
            },
        }),

    updateUserData: async (
        contentId: string,
        data: UserToContentUpdate
    ): Promise<UserToContent> => {
        return apiFetch<UserToContent>(`/content/${contentId}/user-data`, {
            method: 'POST',
            body: JSON.stringify(data),
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

    useMetadataLayers: (id: MaybeRefOrGetter<string | undefined | null>) =>
        useQuery({
            queryKey: ['content', 'metadata-layers', id],
            queryFn: async () => contentApi.getMetadataLayers(toValue(id)!),
            enabled: isEnabled(id),
        }),

    getMetadataLayers: async (contentId: string): Promise<MetadataLayersResponse> => {
        return apiFetch<MetadataLayersResponse>(`/content/${contentId}/metadata-layers`)
    },

    scanContent: async (contentId: string) => {
        return apiFetch(`/content/${contentId}/scan`, { method: 'POST' })
    },

    updateMetadataOverride: async (
        contentId: string,
        data: ContentMetadata
    ): Promise<MetadataLayersResponse> => {
        return apiFetch<MetadataLayersResponse>(`/content/${contentId}/metadata-override`, {
            method: 'POST',
            body: JSON.stringify({ data }),
        })
    },

    listLibraryUris: async (libraryId: string): Promise<LibraryUrisResponse> => {
        return apiFetch<LibraryUrisResponse>(`/content/refs/${libraryId}`)
    },

    useLibraryUris: (libraryId: MaybeRefOrGetter<string | undefined | null>) =>
        useQuery({
            queryKey: ['content', 'library-uris', libraryId],
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
        params: MaybeRefOrGetter<{ search?: string; limit?: number; offset?: number }> = {},
        options: QueryOptions<Paginated<BrokenUserToContent>> = {}
    ) =>
        useQuery({
            queryKey: ['content', 'broken-refs', libraryId, params],
            queryFn: async () => {
                const p = toValue(params)
                const searchParams = new URLSearchParams()
                if (p.search) searchParams.append('search', p.search)
                if (p.limit !== undefined) searchParams.append('limit', String(p.limit))
                if (p.offset !== undefined) searchParams.append('offset', String(p.offset))
                const query = searchParams.toString()
                return apiFetch<Paginated<BrokenUserToContent>>(
                    `/content/broken-refs/${toValue(libraryId)}${query ? `?${query}` : ''}`
                )
            },
            enabled: isEnabled(libraryId),
            ...options,
        }),

    fixBrokenRefs: async (libraryId: string, body: BrokenRefsFixRequest): Promise<void> => {
        await apiFetch(`/content/broken-refs/${libraryId}`, {
            method: 'POST',
            body: JSON.stringify(body),
        })
    },
}
