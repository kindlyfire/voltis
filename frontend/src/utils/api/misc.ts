import { useQuery } from '@tanstack/vue-query'
import { type MaybeRefOrGetter } from 'vue'
import { apiFetch } from '../fetch'
import { isEnabled } from './_utils'

export interface Info {
    version: string
    registration_enabled: boolean
    first_user_flow: boolean
    password_login_enabled: boolean
    oidc_enabled: boolean
    oidc_button_label: string
    oidc_auto_redirect: boolean
}

export const miscApi = {
    useInfo: (enabled: MaybeRefOrGetter<boolean> = true) =>
        useQuery({
            queryKey: ['misc', 'info'],
            queryFn: () => miscApi.info(),
            enabled: isEnabled(enabled),
            refetchOnMount: false,
            retryOnMount: false,
        }),

    info: async (): Promise<Info> => {
        return apiFetch<Info>('/info')
    },
}
