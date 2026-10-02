import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { apiFetch } from '../fetch'
import type { Library, LibraryUpsert, ScanTaskIds } from './types'

export const librariesApi = {
    useList: () =>
        useQuery({
            queryKey: ['libraries'],
            queryFn: async () => apiFetch<Library[]>('/libraries'),
        }),

    useUpsert: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async ({ id, ...body }: LibraryUpsert) =>
                apiFetch<Library>(`/libraries/${id ?? 'new'}`, {
                    method: 'POST',
                    body: JSON.stringify(body),
                }),
            // Resolves once the list has the library, so a scan modal opened next can name it.
            onSuccess: () => queryClient.invalidateQueries({ queryKey: ['libraries'] }),
        })
    },

    useDelete: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (id: string) => apiFetch(`/libraries/${id}`, { method: 'DELETE' }),
            // Resolves once the list no longer has the row, so the dialog can hand focus back.
            onSuccess: () => queryClient.invalidateQueries({ queryKey: ['libraries'] }),
        })
    },

    useScan: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (opts?: { ids?: string[]; force?: boolean; contentIds?: string[] }) =>
                apiFetch<ScanTaskIds>('/libraries/scan', {
                    method: 'POST',
                    body: JSON.stringify({
                        ids: opts?.ids,
                        content_ids: opts?.contentIds,
                        force: opts?.force,
                    }),
                }),
            onSuccess: () => {
                queryClient.invalidateQueries({ queryKey: ['libraries'] })
            },
        })
    },
}
