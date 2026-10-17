import { useMutation, useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { apiFetch } from '../fetch'
import { queryClient } from '../misc'
import type {
    ContentScrobble,
    CountResponse,
    OkResponse,
    RecentScrobble,
    ScrobbleAccount,
    ScrobbleRow,
} from './types'
import { usersApi } from './users'

export const invalidateScrobbleAccount = () =>
    queryClient.invalidateQueries({ queryKey: ['scrobbling', 'account'] })

export const invalidateRecentScrobbles = () =>
    queryClient.invalidateQueries({ queryKey: ['scrobbling', 'recent'] })

const invalidateContent = (id: string) =>
    queryClient.invalidateQueries({ queryKey: ['scrobbling', 'content', id] })

export const scrobbleApi = {
    useAccount: () => {
        const me = usersApi.useMe()
        const meId = computed(() => me.data.value?.id)
        return useQuery({
            // Scoped by user so another account never reads this cache entry.
            queryKey: ['scrobbling', 'account', meId],
            queryFn: async ({ signal }) =>
                (await apiFetch<{ mangabaka: ScrobbleAccount }>('/users/me/scrobbling', { signal }))
                    .mangabaka,
            enabled: () => !!meId.value,
        })
    },

    useConnect: () =>
        useMutation({
            mutationFn: async (token: string) =>
                apiFetch<unknown>('/users/me/scrobbling/mangabaka', {
                    method: 'PUT',
                    body: JSON.stringify({ token }),
                }),
            onSuccess: invalidateScrobbleAccount,
        }),

    usePatch: () =>
        useMutation({
            mutationFn: async (body: { private_new: boolean }) =>
                apiFetch<unknown>('/users/me/scrobbling/mangabaka', {
                    method: 'PATCH',
                    body: JSON.stringify(body),
                }),
            onSuccess: invalidateScrobbleAccount,
        }),

    useDisconnect: () =>
        useMutation({
            mutationFn: async () =>
                apiFetch<OkResponse>('/users/me/scrobbling/mangabaka', { method: 'DELETE' }),
            onSuccess: invalidateScrobbleAccount,
        }),

    useSyncAll: () =>
        useMutation({
            mutationFn: async () =>
                apiFetch<CountResponse>('/users/me/scrobbling/mangabaka/sync-all', {
                    method: 'POST',
                    body: '{}',
                }),
            onSuccess: invalidateScrobbleAccount,
        }),

    list: () => apiFetch<ScrobbleRow[]>('/scrobbles'),

    useRecent: () => {
        const me = usersApi.useMe()
        const meId = computed(() => me.data.value?.id)
        return useQuery({
            queryKey: ['scrobbling', 'recent', meId],
            queryFn: async ({ signal }) =>
                apiFetch<RecentScrobble[]>('/scrobbles/recent', { signal }),
            enabled: () => !!meId.value,
        })
    },

    /** Without ids, every pending series; with ids, those series, failed ones included. */
    send: (ids?: string[]) =>
        apiFetch<OkResponse>('/scrobbles/send', {
            method: 'POST',
            body: JSON.stringify({ content_ids: ids }),
        }),

    /** Reads the user's MangaBaka entry live, so it's fetched only by the series dialog. */
    useContent: (id: MaybeRefOrGetter<string>) => {
        const me = usersApi.useMe()
        const meId = computed(() => me.data.value?.id)
        return useQuery({
            queryKey: ['scrobbling', 'content', () => toValue(id), meId],
            queryFn: async ({ signal }) =>
                apiFetch<ContentScrobble>(`/content/${toValue(id)}/scrobble`, { signal }),
            enabled: () => !!meId.value,
        })
    },

    /** Holding drops what's pending; releasing queues the current state. */
    hold: async (id: string, held: boolean) => {
        const res = await apiFetch<OkResponse>(`/content/${id}/scrobble`, {
            method: 'PUT',
            body: JSON.stringify({ held }),
        })
        void invalidateContent(id)
        return res
    },

    useHold: (id: MaybeRefOrGetter<string>) =>
        useMutation({ mutationFn: async (held: boolean) => scrobbleApi.hold(toValue(id), held) }),

    useSendSeries: (id: MaybeRefOrGetter<string>) =>
        useMutation({
            mutationFn: async (body: { force?: boolean } = {}) =>
                apiFetch<OkResponse>(`/content/${toValue(id)}/scrobble/send`, {
                    method: 'POST',
                    body: JSON.stringify(body),
                }),
            // Not awaited: pending covers only the POST, the dialog waits for the refetch itself.
            onSuccess: () => void invalidateContent(toValue(id)),
        }),
}
