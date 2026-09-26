import { useQuery } from '@tanstack/vue-query'
import type { Ref } from 'vue'
import { apiFetch } from '../fetch'
import { isEnabled } from './_utils'
import type { FolderListing, FsRoots, ResolvedPath } from './types'

export const fsApi = {
    useRoots: () =>
        useQuery({
            queryKey: ['fs', 'roots'],
            queryFn: ({ signal }) => apiFetch<FsRoots>('/fs/roots', { signal }),
            retry: false,
        }),

    useList: (path: Ref<string | null>, hidden: Ref<boolean>) =>
        useQuery({
            queryKey: ['fs', 'list', path, hidden],
            queryFn: ({ signal }) => {
                const params = new URLSearchParams({ path: path.value! })
                if (hidden.value) params.set('hidden', 'true')
                return apiFetch<FolderListing>(`/fs/list?${params}`, { signal })
            },
            enabled: isEnabled(path),
            retry: false,
        }),

    /** At most 100 paths. `nearestExisting` falls back to the closest existing ancestor. */
    resolve: async (paths: string[], opts: { nearestExisting?: boolean } = {}) => {
        const res = await apiFetch<{ results: ResolvedPath[] }>('/fs/resolve', {
            method: 'POST',
            body: JSON.stringify({ paths, nearest_existing: opts.nearestExisting }),
        })
        return res.results
    },
}
