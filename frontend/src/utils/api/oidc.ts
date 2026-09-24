import { useMutation, useQuery } from '@tanstack/vue-query'
import { API_URL, apiFetch } from '../fetch'
import type { Info } from './misc'
import type { OidcPending, OkResponse } from './types'

export const OIDC_LOGIN_URL = `${API_URL}/auth/oidc/login`

let signedOut: boolean | undefined
let signOutVersion = 0
let pendingSignOuts = 0

export function markSignedOut() {
    signedOut = true
    signOutVersion++
    try {
        sessionStorage.setItem('signedOut', '1')
    } catch {}
}

export function isSignedOut(): boolean {
    try {
        signedOut ??= sessionStorage.getItem('signedOut') === '1'
    } catch {}
    return signedOut ?? false
}

export function clearSignedOut() {
    signedOut = false
    try {
        sessionStorage.removeItem('signedOut')
    } catch {}
}

export function beginSignOut() {
    markSignedOut()
    pendingSignOuts++
    return () => {
        pendingSignOuts--
        signOutVersion++
    }
}

export function sessionConfirmation() {
    const version = signOutVersion
    return () => {
        // Session checks overlapping a sign-out cannot cancel its intent.
        if (!pendingSignOuts && version === signOutVersion) clearSignedOut()
    }
}

export function shouldAutoRedirect(
    info: Pick<Info, 'oidc_auto_redirect' | 'first_user_flow'> | undefined,
    hasError: boolean,
    local: unknown,
    afterSignOut: boolean
): boolean {
    return (
        !!info?.oidc_auto_redirect &&
        !info.first_user_flow &&
        !hasError &&
        local !== '1' &&
        !afterSignOut
    )
}

export const oidcApi = {
    usePending: () =>
        useQuery({
            queryKey: ['oidc', 'pending'],
            queryFn: async () => apiFetch<OidcPending>('/auth/oidc/pending'),
            retry: false,
        }),

    useLink: () =>
        useMutation({
            mutationFn: async () =>
                apiFetch<{ url: string }>('/auth/oidc/link', { method: 'POST', body: '{}' }),
        }),

    useConfirm: () =>
        useMutation({
            mutationFn: async (password: string) =>
                apiFetch<OkResponse>('/auth/oidc/confirm', {
                    method: 'POST',
                    body: JSON.stringify({ password }),
                }),
        }),

    useChooseUsername: () =>
        useMutation({
            mutationFn: async (username: string) =>
                apiFetch<OkResponse>('/auth/oidc/choose-username', {
                    method: 'POST',
                    body: JSON.stringify({ username }),
                }),
        }),
}
