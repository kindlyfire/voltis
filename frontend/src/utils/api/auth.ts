import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { apiFetch } from '../fetch'
import type { OkResponse } from './types'

export interface LoginRequest {
    username: string
    password: string
}

export interface RegisterRequest {
    username: string
    password: string
}

export const authApi = {
    useLogin: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (credentials: LoginRequest) =>
                apiFetch<OkResponse>('/auth/login', {
                    method: 'POST',
                    body: JSON.stringify(credentials),
                }),
            onSuccess: () => {
                queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
            },
        })
    },

    useRegister: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async (credentials: RegisterRequest) =>
                apiFetch<OkResponse>('/auth/register', {
                    method: 'POST',
                    body: JSON.stringify(credentials),
                }),
            onSuccess: () => {
                queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
            },
        })
    },

    useLogout: () => {
        const queryClient = useQueryClient()
        return useMutation({
            mutationFn: async () =>
                apiFetch<OkResponse>('/auth/logout', {
                    method: 'POST',
                }),
            onSuccess: () => {
                queryClient.invalidateQueries()
            },
        })
    },
}
