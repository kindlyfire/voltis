import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { apiFetch } from '../fetch'
import { beginSignOut, clearSignedOut } from './oidc'
import type { LogoutResponse, OkResponse } from './types'

export interface LoginRequest {
    username: string
    password: string
}

export interface RegisterRequest {
    username: string
    password: string
}

export interface AppCodeRequest {
    code_challenge: string
    client_name: string
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
                clearSignedOut()
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
                clearSignedOut()
                queryClient.invalidateQueries({ queryKey: ['users', 'me'] })
            },
        })
    },

    useLogout: () => {
        const queryClient = useQueryClient()
        return useMutation({
            onMutate: beginSignOut,
            onSettled: (_data, _error, _variables, endSignOut) => endSignOut?.(),
            mutationFn: async () =>
                apiFetch<LogoutResponse>('/auth/logout', {
                    method: 'POST',
                    body: '{}',
                }),
            onSuccess: () => {
                // Invalidating alone would keep the raw keys cached and refetch them.
                queryClient.removeQueries({ queryKey: ['app-keys'] })
                queryClient.removeQueries({ queryKey: ['users', 'sessions'] })
                queryClient.invalidateQueries()
            },
        })
    },

    useAppCode: () =>
        useMutation({
            mutationFn: async (req: AppCodeRequest) =>
                apiFetch<{ redirect_url: string }>('/auth/app-code', {
                    method: 'POST',
                    body: JSON.stringify(req),
                }),
        }),
}
