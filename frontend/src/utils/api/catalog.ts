import type { QueryClient, QueryKey } from '@tanstack/vue-query'
import { hashKey } from '@tanstack/vue-query'
import { useThrottleFn } from '@vueuse/core'
import type { MaybeRefOrGetter } from 'vue'
import { ws } from '../ws'

// Catalog queries, keyed under ['content'] or ['metadata'], read what scans and metadata changes
// touch. Candidates and config are keyed outside them: neither changes with the catalog, and
// candidates cost provider requests.
const isCatalog = (key: QueryKey) => key[0] === 'content' || key[0] === 'metadata'

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
    return client.invalidateQueries({
        predicate: ({ queryKey }) => isCatalog(queryKey) && hashKey(queryKey) !== skip,
    })
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
            client.invalidateQueries({
                predicate: ({ queryKey }) => {
                    if (!isCatalog(queryKey)) return false
                    const lib = keyLibrary(queryKey)
                    return libs.has(undefined) || lib === undefined || libs.has(lib)
                },
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
