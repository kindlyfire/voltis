import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { apiFetch, RequestError } from '../fetch'
import { queryClient } from '../misc'
import { ws } from '../ws'
import type { UpdateMe, User, UserUpsert } from './types'

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
