import type { Query, QueryClient, QueryKey } from '@tanstack/vue-query'
import { hashKey } from '@tanstack/vue-query'
import { useThrottleFn } from '@vueuse/core'
import type { MaybeRefOrGetter } from 'vue'
import { ws } from '../ws'

// Catalog queries, keyed under ['content'] or ['metadata'], read what scans and metadata changes
// touch. Candidates and config are keyed outside them: neither changes with the catalog, and
// candidates cost provider requests.
const isCatalog = (key: QueryKey) => key[0] === 'content' || key[0] === 'metadata'

/** The content grid's buckets and pages, keyed ['content', 'list', scope, 'window', ...]. */
export const isContentWindow = (key: QueryKey) => key[1] === 'list' && key[3] === 'window'

// Follow-up refetches, one per query that was fetching when invalidated.
const queued = new WeakMap<Query, Promise<void>>()

/**
 * Invalidates the matching queries. A query that is fetching finishes first, and is then fetched
 * once more: its response may predate the write, and cancelling it instead would starve a query
 * slower than the invalidations.
 */
export async function invalidateMatching(
    client: QueryClient,
    predicate: (key: QueryKey) => boolean
): Promise<void> {
    const invalidate = (match: (q: Query) => boolean) =>
        client.invalidateQueries({ predicate: match }, { cancelRefetch: false })
    const isFetching = (q: Query) => q.state.fetchStatus !== 'idle' && !!q.promise
    const fetching = client
        .getQueryCache()
        .findAll({ predicate: q => predicate(q.queryKey) && isFetching(q) })
    // One pass over the cache for the idle ones: a pass per query is quadratic in cached pages.
    const idle = invalidate(q => predicate(q.queryKey) && !isFetching(q))
    const followUps = fetching.map(q => {
        q.invalidate()
        const existing = queued.get(q)
        if (existing) return existing
        const next = q
            .promise!.catch(() => {})
            .then(() => {
                // Deleted first, so a change during the follow-up queues another.
                queued.delete(q)
                return invalidate(current => current === q)
            })
        queued.set(q, next)
        return next
    })
    await Promise.all([idle, ...followUps])
}

/** The third segment of a catalog query key that reads one library; none reads any. */
export const libraryScope = (libraryId: MaybeRefOrGetter<string | null | undefined>) => ({
    library: libraryId,
})

function keyLibrary(key: QueryKey): string | undefined {
    const scope = key[2]
    if (typeof scope !== 'object' || scope === null || !('library' in scope)) return undefined
    return (scope.library as string | null | undefined) ?? undefined
}

/** Refetches every catalog query but `except`, whose data the caller just set. */
export function refetchCatalog(client: QueryClient, except?: QueryKey): Promise<void> {
    const skip = except && hashKey(except)
    return invalidateMatching(client, key => isCatalog(key) && hashKey(key) !== skip)
}

/**
 * Refetches, as the server reports changes, the libraries and the catalog queries that read a
 * changed library or no one library; throttled to one refetch per 500ms.
 */
export function syncCatalog(client: QueryClient): void {
    // Libraries changed since the last refetch; undefined stands for any.
    const changed = new Set<string | undefined>()
    const refetch = useThrottleFn(
        () => {
            const libs = new Set(changed)
            changed.clear()
            client.invalidateQueries({ queryKey: ['libraries'] })
            void invalidateMatching(client, key => {
                if (!isCatalog(key)) return false
                const lib = keyLibrary(key)
                return libs.has(undefined) || lib === undefined || libs.has(lib)
            })
        },
        500,
        true,
        true
    )
    const onChange = (libraryId?: string) => {
        changed.add(libraryId)
        void refetch()
    }
    ws.on('catalog_changed', (msg: { library_id?: string }) => onChange(msg.library_id))
    ws.on('$open', () => onChange())
}
