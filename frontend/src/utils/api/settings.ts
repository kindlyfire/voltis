import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { apiFetch } from '../fetch'
import type { ProxyAuthStatus, Setting } from './types'

export type SettingsPatch = Record<string, boolean | number | string | string[]>

export const settingsApi = {
    useList: () =>
        useQuery({
            queryKey: ['settings'],
            queryFn: async () => apiFetch<Setting[]>('/settings'),
        }),

    useProxyAuth: () =>
        useQuery({
            queryKey: ['settings', 'proxy-auth'],
            queryFn: async () => apiFetch<ProxyAuthStatus>('/settings/proxy-auth'),
        }),

    useUpdate: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (patch: SettingsPatch) =>
                apiFetch<Setting[]>('/settings', {
                    method: 'POST',
                    body: JSON.stringify(patch),
                }),
            onSuccess: () => {
                queryClient.invalidateQueries({ queryKey: ['settings'] })
                queryClient.invalidateQueries({ queryKey: ['misc', 'info'] })
                // can_logout comes from the proxy logout URL.
                queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
            },
        })
    },
}

export function settingValue<T>(settings: Setting[] | undefined, key: string, fallback: T): T {
    const found = settings?.find(s => s.key === key)
    return found?.value === undefined || found.value === null ? fallback : (found.value as T)
}
