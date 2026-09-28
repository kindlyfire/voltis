import {
    keepPreviousData,
    type QueryClient,
    useMutation,
    useQuery,
    useQueryClient,
} from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { apiFetch } from '../fetch'
import { ws } from '../ws'
import { isEnabled } from './_utils'
import { libraryScope, refetchCatalog } from './catalog'
import type { Content, Paginated } from './types'

/** Metadata keyed by field; the fields come from `/metadata/config`. */
export type MetadataValues = Record<string, unknown>

export interface StaffEntry {
    name: string
    role: string
}

export interface MetadataLinkRef {
    label: string
    url: string
}

/** The merged fields the read-only views show. */
export interface DisplayMetadata {
    title?: string
    alt_titles?: string[]
    description?: string
    staff?: StaffEntry[]
    publishers?: string[]
    language?: string
    /** Partial ISO: YYYY, YYYY-MM, or YYYY-MM-DD. */
    publication_date?: string
    genres?: string[]
    tags?: string[]
    content_rating?: string
    status?: string
    kind?: string
    /** 0-100. */
    rating?: number
    links?: MetadataLinkRef[]
}

export type FieldType =
    | 'string'
    | 'text'
    | 'int'
    | 'float'
    | 'date'
    | 'string_list'
    | 'genre_list'
    | 'staff'
    | 'enum'
    | 'links'
    | 'cover'

export interface FieldDef {
    key: string
    label: string
    type: FieldType
    /** Enum values; staff roles. */
    options?: string[]
    min?: number
    max?: number
    content_types: string[]
    editable: boolean
}

export interface ProviderInfo {
    name: string
    label: string
    content_types: string[]
}

export interface MetadataConfig {
    fields: FieldDef[]
    providers: ProviderInfo[]
}

export interface MetadataLayerView {
    source: string
    label: string
    kind: 'file' | 'provider' | 'overrides'
    /** In the overrides layer, `null` clears a field. */
    fields: MetadataValues
    /** A provider's snapshot. */
    raw?: unknown
}

export interface EntrySummary {
    key: { provider: string; id: string }
    title: string
    year: number | null
    kind: string
    status: string
    staff: string[]
    cover_url: string
    url: string
}

export interface Evaluation {
    title: number
    exact: boolean
    year?: 'match' | 'conflict'
    staff?: 'match' | 'conflict'
    volumes?: 'conflict'
    score: number
    eligible: boolean
}

export interface Candidate extends EntrySummary {
    evaluation: Evaluation
}

export interface MetadataLink {
    provider: string
    label: string
    /** Null while there is no row; sent back as `expect_rev`. */
    rev: number | null
    state: 'none' | 'review' | 'unmatched' | 'linked' | 'ignored'
    external_id: string | null
    origin: 'auto' | 'manual' | null
    entry: EntrySummary | null
    deleted: boolean
    candidates: Candidate[]
    rejected: string[]
    fetched_at: string | null
    /** The last match failed, or, linked, the stored entry can't be read. */
    last_error: string | null
    /** The linked entry's next refresh. */
    refresh_at: string | null
    /** Refreshes of the linked entry that failed since the last success. */
    refresh_attempts: number
    refresh_error: string | null
}

export interface MetadataView {
    /** The content type, which selects the fields. */
    type: string
    overrides_rev: number
    merged: MetadataValues
    sources: Record<string, string[]>
    layers: MetadataLayerView[]
    links: MetadataLink[]
}

/** The series' own title, to search providers by: not the one a linked provider gave it. */
export function ownTitle(view: MetadataView): string {
    const title =
        view.layers.find(l => l.kind === 'overrides')?.fields.title ??
        view.layers.find(l => l.kind === 'file')?.fields.title
    return typeof title === 'string' ? title : ''
}

export type ReviewTab = 'review' | 'unmatched' | 'auto' | 'ignored'

export interface ReviewItem {
    content: Content
    /** The title matching reads: the series' own, without providers. */
    local_title: string
    /** Actions send `content.id`, `link.provider`, and `link.rev`. */
    link: MetadataLink
}

/** Links waiting for an admin in a library; `failed` counts unmatched ones whose last attempt failed. */
export interface ReviewSummary {
    library_id: string
    review: number
    unmatched: number
    failed: number
}

export interface MatchCounts {
    linked: number
    review: number
    unmatched: number
    /** The provider failed; tried again with backoff. */
    failed: number
    /** Changed during the lookup. */
    skipped: number
}

export interface RefreshCounts {
    refreshed: number
    failed: number
}

/** A run of the worker's rounds that had work. */
export interface WorkerPass<T> {
    /** All zero: none yet. */
    counts: T
    /** Null while it runs. */
    finished: string | null
}

/** What background matching and refreshing is doing. */
export interface WorkerStatus {
    activity: 'idle' | 'recomputing' | 'matching' | 'refreshing'
    /** The library being matched. */
    library_id?: string
    /** Rows left to recompute for a new metadata version. */
    stale: number
    paused: boolean
    /** Since the server started. */
    matched: MatchCounts
    refreshed: RefreshCounts
    /** The running or last pass. */
    match_pass: WorkerPass<MatchCounts>
    refresh_pass: WorkerPass<RefreshCounts>
}

/** How refreshing a provider's entries goes. */
export interface ProviderHealth {
    provider: string
    /** Entries whose last refresh failed. */
    failing: number
    /** Linked series whose stored entry can't be read. */
    undecodable: number
    /** The latest entry fetched. */
    last_fetched: string | null
}

export interface MetadataSummary {
    libraries: ReviewSummary[]
    providers: ProviderHealth[]
    worker: WorkerStatus
}

export interface ReviewAction {
    content_id: string
    provider: string
    action: 'link' | 'reject' | 'ignore' | 'undo'
    external_id?: string
    /** Reject: the candidates none of which fits. */
    external_ids?: string[]
    /** Undo: the revision the decision saved. */
    expect_rev: number | null
}

export interface ReviewResult {
    content_id: string
    provider: string
    ok: boolean
    /** The revision saved, which undoes it. */
    rev?: number
    error?: string
}

export type LinkAction =
    | {
          action: 'link'
          contentId: string
          provider: string
          externalId: string
          rev: number | null
      }
    | {
          action: 'reject'
          contentId: string
          provider: string
          externalIds: string[]
          rev: number | null
      }
    | {
          action: 'ignore' | 'rematch'
          contentId: string
          provider: string
          rev: number | null
      }
    | { action: 'refresh'; contentId: string; provider: string }
    /** Restores the link as the decision that saved `rev` found it. */
    | { action: 'undo'; contentId: string; provider: string; rev: number }

const viewKey = (contentId: MaybeRefOrGetter<string | undefined | null>) => [
    'content',
    'metadata',
    contentId,
]
const summaryKey = ['metadata', 'summary']

/** Stores the returned view, and refetches the catalog, which shows it, but not the view itself. */
function onChanged(client: QueryClient, contentId: string, view: MetadataView) {
    const key = viewKey(contentId)
    client.setQueryData(key, view)
    void refetchCatalog(client, key)
}

export const revOf = (view: MetadataView, provider: string) =>
    view.links.find(l => l.provider === provider)!.rev!

/** A plain function, as the toast undoing a decision outlives the component that made it. */
export async function linkAction(client: QueryClient, a: LinkAction): Promise<MetadataView> {
    const view = await apiFetch<MetadataView>(`/metadata/content/${a.contentId}/${a.action}`, {
        method: 'POST',
        body: JSON.stringify({
            provider: a.provider,
            external_id: a.action === 'link' ? a.externalId : undefined,
            external_ids: a.action === 'reject' ? a.externalIds : undefined,
            expect_rev: a.action === 'refresh' ? undefined : a.rev,
        }),
    })
    onChanged(client, a.contentId, view)
    return view
}

/** Applies each item on its own; the results say how each went. */
export async function resolveReview(client: QueryClient, items: ReviewAction[]) {
    const res = await apiFetch<{ results: ReviewResult[] }>('/metadata/review/resolve', {
        method: 'POST',
        body: JSON.stringify({ items }),
    })
    void refetchCatalog(client)
    return res
}

export const metadataApi = {
    useConfig: () =>
        useQuery({
            queryKey: ['metadata-config'],
            queryFn: ({ signal }) => apiFetch<MetadataConfig>('/metadata/config', { signal }),
            staleTime: Infinity,
        }),

    useContent: (id: MaybeRefOrGetter<string | undefined | null>) =>
        useQuery({
            queryKey: viewKey(id),
            queryFn: ({ signal }) =>
                apiFetch<MetadataView>(`/metadata/content/${toValue(id)}`, { signal }),
            enabled: isEnabled(id),
        }),

    useSaveOverrides: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: (v: { contentId: string; rev: number; fields: MetadataValues }) =>
                apiFetch<MetadataView>(`/metadata/content/${v.contentId}/overrides`, {
                    method: 'POST',
                    body: JSON.stringify({ rev: v.rev, fields: v.fields }),
                }),
            onSuccess: (view, v) => onChanged(queryClient, v.contentId, view),
        })
    },

    /** `q` is a title, an ID, or a URL; empty searches the series' own title. */
    useCandidates: (
        contentId: MaybeRefOrGetter<string>,
        provider: MaybeRefOrGetter<string>,
        q: MaybeRefOrGetter<string | null>
    ) =>
        useQuery({
            queryKey: ['metadata-candidates', contentId, provider, q],
            queryFn: ({ signal }) => {
                const params = new URLSearchParams({
                    provider: toValue(provider),
                    q: toValue(q)!,
                })
                return apiFetch<{ data: Candidate[] }>(
                    `/metadata/content/${toValue(contentId)}/candidates?${params}`,
                    { signal }
                )
            },
            enabled: isEnabled(q),
        }),

    useLinkAction: () => {
        const queryClient = useQueryClient()
        return useMutation({ mutationFn: (a: LinkAction) => linkAction(queryClient, a) })
    },

    useReview: (
        params: MaybeRefOrGetter<{
            tab: ReviewTab
            libraryId?: string | null
            /** Words of the series' titles. */
            search?: string
            /** Only failed matches, or linked entries that can't be read. */
            failed?: boolean
            limit: number
            offset: number
        }>
    ) =>
        useQuery({
            queryKey: ['metadata', 'review', libraryScope(() => toValue(params).libraryId), params],
            queryFn: ({ signal }) => {
                const { tab, libraryId, search, failed, limit, offset } = toValue(params)
                const query = new URLSearchParams({
                    tab,
                    limit: String(limit),
                    offset: String(offset),
                })
                if (libraryId) query.set('library_id', libraryId)
                if (search) query.set('q', search)
                if (failed) query.set('failed', 'true')
                return apiFetch<Paginated<ReviewItem>>(`/metadata/review?${query}`, {
                    signal,
                })
            },
            placeholderData: keepPreviousData,
        }),

    useResolveReview: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: (items: ReviewAction[]) => resolveReview(queryClient, items),
        })
    },

    useSummary: (options?: { enabled?: MaybeRefOrGetter<boolean> }) =>
        useQuery({
            queryKey: summaryKey,
            queryFn: ({ signal }) => apiFetch<MetadataSummary>('/metadata/summary', { signal }),
            enabled: options?.enabled,
        }),

    /** Makes the libraries' unmatched series due for matching now; all libraries by default. */
    useMatchNow: () =>
        useMutation({
            mutationFn: (libraryIds?: string[]) =>
                apiFetch('/metadata/match', {
                    method: 'POST',
                    body: JSON.stringify({ library_ids: libraryIds }),
                }),
        }),

    /** Makes all provider data that series use due for a refresh now. */
    useRefreshNow: () =>
        useMutation({
            mutationFn: () => apiFetch('/metadata/refresh', { method: 'POST' }),
        }),
}

/** Keeps the summary's worker status current as the server reports it. */
export function syncWorkerStatus(client: QueryClient): void {
    ws.on('metadata_status', (msg: { status: WorkerStatus }) =>
        client.setQueryData<MetadataSummary>(
            summaryKey,
            summary => summary && { ...summary, worker: msg.status }
        )
    )
}
