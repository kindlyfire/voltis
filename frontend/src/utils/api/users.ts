import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { apiFetch, RequestError } from '../fetch'
import { ws } from '../ws'
import type { UpdateMe, User, UserUpsert } from './types'

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
                try {
                    const u = await apiFetch<User>('/users/me')
                    if (u?.id) {
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
        }),

    useUpdateMe: () => {
        const queryClient = useQueryClient()
        return useMutation({
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

    useUpsert: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (user: UserUpsert) => {
                const url = `/users/${user.id ?? 'new'}`
                const { id: _, ...body } = user
                return apiFetch<User>(url, {
                    method: 'POST',
                    body: JSON.stringify(body),
                })
            },
            onSuccess: () => {
                queryClient.invalidateQueries({ queryKey: ['users'] })
            },
        })
    },

    useDelete: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (id: string) => apiFetch(`/users/${id}`, { method: 'DELETE' }),
            onSuccess: () => {
                queryClient.invalidateQueries({ queryKey: ['users'] })
            },
        })
    },
}
