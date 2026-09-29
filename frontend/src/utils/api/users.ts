import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { apiFetch, RequestError } from '../fetch'
import { queryClient } from '../misc'
import { ws } from '../ws'
import { beginSignOut, sessionConfirmation } from './oidc'
import type {
    Identity,
    IdentityLink,
    Me,
    PreferencesPatch,
    UpdateMe,
    User,
    UserUpsert,
} from './types'

/** Shared by every mutation that writes `['users','me']`, so their responses
 * can't be applied out of order: an unscoped POST landing after a slow PATCH
 * would otherwise restore the pre-POST user and stick (`refetchOnMount: false`). */
const ME_SCOPE = { id: 'users-me' }

const revalidateMe = () => queryClient.invalidateQueries({ queryKey: ['users', 'me'] })

ws.on('$open', revalidateMe)
ws.on('$close', revalidateMe)

export const usersApi = {
    useList: () =>
        useQuery({
            queryKey: ['users'],
            queryFn: async () => apiFetch<User[]>('/users'),
        }),

    useMe: () =>
        useQuery({
            queryKey: ['users', 'me'],
            queryFn: async () => {
                const confirmSession = sessionConfirmation()
                try {
                    const u = await apiFetch<Me>('/users/me')
                    if (u?.id) {
                        confirmSession()
                        ws.connect()
                    }
                    return u
                } catch (e) {
                    if (e instanceof RequestError && e.response?.status === 401) {
                        return null
                    }
                    throw e
                }
            },
            refetchOnMount: false,
            // Consumers mount once it loads; an errored one would refetch on each mount (Retry refetches).
            retryOnMount: false,
        }),

    useUpdateMe: () => {
        const queryClient = useQueryClient()
        return useMutation({
            scope: ME_SCOPE,
            mutationFn: async (body: UpdateMe) => {
                return apiFetch<User>('/users/me', {
                    method: 'POST',
                    body: JSON.stringify(body),
                })
            },
            onSuccess: () => {
                queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
            },
        })
    },

    usePatchPreferences: () => {
        const queryClient = useQueryClient()
        return useMutation({
            scope: ME_SCOPE,
            mutationFn: async (body: PreferencesPatch) =>
                apiFetch<User>('/users/me/preferences', {
                    method: 'PATCH',
                    body: JSON.stringify(body),
                }),
            onSuccess: async user => {
                // The ws `$open`/`$close` invalidation can start a GET while
                // this PATCH is in flight. Cancelling here, rather than in
                // `onMutate`, stops its stale response from landing after
                // `setQueryData` and sticking.
                await queryClient.cancelQueries({ queryKey: ['users', 'me'] })
                queryClient.setQueryData(['users', 'me'], user)
            },
        })
    },

    useUpsert: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async ({ id, ...body }: UserUpsert) =>
                apiFetch<User>(`/users/${id ?? 'new'}`, {
                    method: 'POST',
                    body: JSON.stringify(body),
                }),
            onSuccess: () => {
                queryClient.invalidateQueries({ queryKey: ['users'] })
            },
        })
    },

    useIdentities: (userId: MaybeRefOrGetter<string>) =>
        useQuery({
            queryKey: ['users', 'identities', () => toValue(userId)],
            queryFn: async () => apiFetch<Identity[]>(`/users/${toValue(userId)}/identities`),
        }),

    useUnlinkIdentity: (userId: MaybeRefOrGetter<string>) => {
        const queryClient = useQueryClient()
        return useMutation({
            onMutate: () => {
                if (toValue(userId) === 'me') return beginSignOut()
            },
            onSettled: (_data, _error, _variables, endSignOut) => endSignOut?.(),
            mutationFn: async (identityId: string) =>
                apiFetch<unknown>(`/users/${toValue(userId)}/identities/${identityId}`, {
                    method: 'DELETE',
                    body: '{}',
                }),
            onSuccess: () => {
                queryClient.invalidateQueries({ queryKey: ['users'] })
            },
        })
    },

    useLinkIdentity: (userId: MaybeRefOrGetter<string>) => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (body: IdentityLink) =>
                apiFetch<Identity[]>(`/users/${toValue(userId)}/identities`, {
                    method: 'POST',
                    body: JSON.stringify(body),
                }),
            onSuccess: () => {
                queryClient.invalidateQueries({ queryKey: ['users'] })
            },
        })
    },

    useDelete: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (id: string) => apiFetch(`/users/${id}`, { method: 'DELETE' }),
            // Resolves once the list no longer has the row, so the dialog can hand focus back.
            onSuccess: () => queryClient.invalidateQueries({ queryKey: ['users'] }),
        })
    },
}
