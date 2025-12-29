import { useMutation, useQuery, useQueryClient, type UseQueryOptions } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter, type UnwrapRef } from 'vue'
import { apiFetch } from '../fetch'
import type {
    CustomList,
    CustomListDetail,
    CustomListEntry,
    CustomListEntryCreate,
    CustomListEntryUpdate,
    CustomListReorderRequest,
    CustomListUpsert,
    OkResponse,
} from './types'
import { isEnabled } from './_utils'

type QueryOptions<T> = Omit<UnwrapRef<UseQueryOptions<T>>, 'queryKey' | 'queryFn'>

const listsKey = ['custom-lists']
const detailKey = (id: string | undefined | null) => [...listsKey, id]

export const customListsApi = {
    useList: (
        userFilter: MaybeRefOrGetter<'all' | 'me' | 'others'> = 'all',
        options: QueryOptions<CustomList[]> = {}
    ) =>
        useQuery({
            queryKey: [...listsKey, 'list', userFilter],
            queryFn: async () => {
                const filter = toValue(userFilter) ?? 'all'
                const params = filter ? `?user=${filter}` : ''
                return apiFetch<CustomList[]>(`/custom-lists${params}`)
            },
            enabled: isEnabled(userFilter),
            ...options,
        }),

    useGet: (
        id: MaybeRefOrGetter<string | undefined | null>,
        options: QueryOptions<CustomListDetail> = {}
    ) =>
        useQuery({
            queryKey: computed(() => detailKey(toValue(id)!)),
            queryFn: async () => customListsApi.get(toValue(id)!),
            enabled: isEnabled(id),
            ...options,
        }),

    get: async (id: string) => {
        return apiFetch<CustomListDetail>(`/custom-lists/${id}`)
    },

    useCreate: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (body: CustomListUpsert) =>
                apiFetch<CustomList>(`/custom-lists`, {
                    method: 'POST',
                    body: JSON.stringify(body),
                }),
            onSuccess: () => {
                queryClient.invalidateQueries({ queryKey: listsKey })
            },
        })
    },

    useUpdate: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (data: CustomListUpsert & { id: string }) => {
                const { id, ...body } = data
                return apiFetch<CustomList>(`/custom-lists/${id}`, {
                    method: 'POST',
                    body: JSON.stringify(body),
                })
            },
            onSuccess: (_data, variables) => {
                queryClient.invalidateQueries({ queryKey: listsKey })
                queryClient.invalidateQueries({ queryKey: detailKey(variables.id) })
            },
        })
    },

    useDelete: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (id: string) =>
                apiFetch<OkResponse>(`/custom-lists/${id}`, { method: 'DELETE' }),
            onSuccess: (_data, id) => {
                queryClient.invalidateQueries({ queryKey: listsKey })
                queryClient.invalidateQueries({ queryKey: detailKey(id) })
            },
        })
    },

    useCreateEntry: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (data: CustomListEntryCreate & { listId: string }) => {
                const { listId, ...body } = data
                return apiFetch<CustomListEntry>(`/custom-lists/${listId}/entries`, {
                    method: 'POST',
                    body: JSON.stringify(body),
                })
            },
            onSuccess: (_data, variables) => {
                queryClient.invalidateQueries({ queryKey: detailKey(variables.listId) })
                queryClient.invalidateQueries({ queryKey: listsKey })
            },
        })
    },

    useUpdateEntry: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (
                data: CustomListEntryUpdate & { listId: string; entryId: string }
            ) => {
                const { listId, entryId, ...body } = data
                return apiFetch<CustomListEntry>(`/custom-lists/${listId}/entries/${entryId}`, {
                    method: 'POST',
                    body: JSON.stringify(body),
                })
            },
            onSuccess: (_data, variables) => {
                queryClient.invalidateQueries({ queryKey: detailKey(variables.listId) })
                queryClient.invalidateQueries({ queryKey: listsKey })
            },
        })
    },

    useDeleteEntry: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (data: { listId: string; entryId: string }) =>
                apiFetch<OkResponse>(`/custom-lists/${data.listId}/entries/${data.entryId}`, {
                    method: 'DELETE',
                }),
            onSuccess: (_data, variables) => {
                queryClient.invalidateQueries({ queryKey: detailKey(variables.listId) })
                queryClient.invalidateQueries({ queryKey: listsKey })
            },
        })
    },

    useReorderEntries: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (data: CustomListReorderRequest & { listId: string }) => {
                const { listId, ...body } = data
                return apiFetch<OkResponse>(`/custom-lists/${listId}/entries/reorder`, {
                    method: 'POST',
                    body: JSON.stringify(body),
                })
            },
            onSuccess: (_data, variables) => {
                queryClient.invalidateQueries({ queryKey: detailKey(variables.listId) })
                queryClient.invalidateQueries({ queryKey: listsKey })
            },
        })
    },
}
