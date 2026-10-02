import { useMutation, useQuery, type Query } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { apiFetch, RequestError } from '../fetch'
import { queryClient } from '../misc'
import { isEnabled } from './_utils'
import { invalidateMatching, libraryScope } from './catalog'
import { listSearchParams } from './content'
import type {
    Content,
    ContinueEntry,
    ContinueTarget,
    CountResponse,
    ReadingRequest,
    ReadingResponse,
    ReadingStateResponse,
    SeriesReadingRequest,
    UncountedPage,
} from './types'

/** A write another tab or device overtook: `current` is what the server has now. */
export class ReadingConflict extends Error {
    constructor(readonly current: ReadingStateResponse) {
        super('Changed on another device')
        this.name = 'ReadingConflict'
    }
}

/** Home's rows, under ['content', 'list'] so list invalidations cover them too. The readers keep
 * sibling lists active there, so these are matched on their own. */
const homeRowFilters = {
    queryKey: ['content', 'list'],
    predicate: (q: Query) =>
        q.queryKey[3] === 'continue-reading' || q.queryKey[3] === 'recently-updated',
}

export async function invalidateContinueReading() {
    // Without data, invalidating joins an in-flight fetch, whose response may predate the write.
    await queryClient.cancelQueries(homeRowFilters)
    await queryClient.invalidateQueries(homeRowFilters)
}

/** Moves `item`'s entry to the front so HomePage mounts in the new order; the refetch adds missing ones. */
export function bumpContinueReading(item: Content) {
    queryClient.setQueriesData<ContinueEntry[]>(
        { queryKey: ['content', 'list'], predicate: q => q.queryKey[3] === 'continue-reading' },
        entries => {
            if (!entries) return entries
            const i = entries.findIndex(e =>
                e.series ? e.series.id === item.parent_id : e.item.id === item.id
            )
            return i > 0 ? [entries[i]!, ...entries.toSpliced(i, 1)] : entries
        }
    )
}

const readingKeys = (contentId: string, parentId: string | null) => (key: readonly unknown[]) =>
    key[0] === 'content' && [contentId, parentId, 'list'].includes(key[1] as string)

/** Refetches what a reading write can change: the item (and its continue target), its series and
 * every list, after any fetch already under way. */
export function invalidateReading(contentId: string, parentId: string | null) {
    return invalidateMatching(queryClient, readingKeys(contentId, parentId))
}

/** After a saved position: Home's rows refetch, and the rest refetches when next used. */
export function markPositionSaved(contentId: string, parentId: string | null) {
    void invalidateContinueReading()
    void queryClient.invalidateQueries({
        predicate: q => readingKeys(contentId, parentId)(q.queryKey),
        refetchType: 'none',
    })
}

export const readingApi = {
    get: (id: string, init?: RequestInit) =>
        apiFetch<ReadingStateResponse>(`/content/${id}/reading`, init),

    /** Rejects with a ReadingConflict when another writer got there first. */
    post: async (id: string, body: ReadingRequest, init?: RequestInit) => {
        try {
            return await apiFetch<ReadingResponse>(`/content/${id}/reading`, {
                method: 'POST',
                body: JSON.stringify(body),
                ...init,
            })
        } catch (err) {
            const current = (err as RequestError).json as ReadingStateResponse | undefined
            if (err instanceof RequestError && err.response?.status === 409 && current?.state) {
                throw new ReadingConflict(current)
            }
            throw err
        }
    },

    /** A page command, last writer wins. Resolves once the affected queries have refetched. */
    useCommand: () =>
        useMutation({
            mutationFn: ({ contentId, request }: { contentId: string; request: ReadingRequest }) =>
                readingApi.post(contentId, request),
            onSuccess: async (_res, { contentId }) => {
                const parentId = queryClient.getQueryData<Content>([
                    'content',
                    contentId,
                ])?.parent_id
                await invalidateReading(contentId, parentId ?? null)
            },
        }),

    /** Returns how many items changed, once the content queries have refetched. */
    seriesReading: async (seriesId: string, body: SeriesReadingRequest) => {
        const res = await apiFetch<CountResponse>(`/content/${seriesId}/series-reading`, {
            method: 'POST',
            body: JSON.stringify(body),
        })
        await invalidateMatching(queryClient, key => key[0] === 'content')
        return res
    },

    useContinue: (id: MaybeRefOrGetter<string | null | undefined>) =>
        useQuery({
            queryKey: ['content', id, 'continue'],
            queryFn: async () => apiFetch<ContinueTarget>(`/content/${toValue(id)}/continue`),
            enabled: isEnabled(id),
        }),

    useContinueReading: (limit = 10) =>
        useQuery({
            queryKey: ['content', 'list', libraryScope(undefined), 'continue-reading', limit],
            queryFn: async ({ signal }) =>
                apiFetch<ContinueEntry[]>(`/content/continue-reading?limit=${limit}`, { signal }),
        }),

    useRecentlyUpdated: (limit = 10) =>
        useQuery({
            queryKey: ['content', 'list', libraryScope(undefined), 'recently-updated', limit],
            queryFn: async ({ signal }) => {
                const q = listSearchParams({ sort: 'recently_updated', sort_order: 'desc', limit })
                q.set('count', 'false')
                return apiFetch<UncountedPage<Content>>(`/content?${q}`, { signal })
            },
        }),
}
