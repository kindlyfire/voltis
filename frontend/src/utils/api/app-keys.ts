import { useMutation, useQuery } from '@tanstack/vue-query'
import { computed } from 'vue'
import { apiFetch } from '../fetch'
import { queryClient } from '../misc'
import type { AppKey } from './types'
import { usersApi } from './users'

const invalidateKeys = () => {
    void queryClient.invalidateQueries({ queryKey: ['app-keys'] })
}

export const appKeysApi = {
    useKeys: () => {
        const me = usersApi.useMe()
        const meId = computed(() => me.data.value?.id)
        return useQuery({
            // Scoped by user so another account never reads this cache entry.
            queryKey: ['app-keys', meId],
            queryFn: async ({ signal }) => apiFetch<AppKey[]>('/users/me/app-keys', { signal }),
            enabled: () => !!meId.value,
            // Raw keys leave the cache as soon as the page unmounts.
            gcTime: 0,
        })
    },

    useCreateKey: () =>
        useMutation({
            mutationFn: async (name: string) =>
                apiFetch<AppKey>('/users/me/app-keys', {
                    method: 'POST',
                    body: JSON.stringify({ name }),
                }),
            onSuccess: invalidateKeys,
            gcTime: 0,
        }),

    useRevokeKey: () =>
        useMutation({
            mutationFn: async (id: string) =>
                apiFetch<unknown>(`/users/me/app-keys/${id}`, { method: 'DELETE', body: '{}' }),
            onSuccess: invalidateKeys,
        }),
}
