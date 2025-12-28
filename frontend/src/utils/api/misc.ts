import { useQuery } from '@tanstack/vue-query'
import { type MaybeRefOrGetter } from 'vue'
import { apiFetch } from '../fetch'
import { isEnabled } from './_utils'

export interface Info {
	version: string
	registration_enabled: boolean
}

export const miscApi = {
	useInfo: (enabled: MaybeRefOrGetter<boolean> = true) =>
		useQuery({
			queryKey: ['misc', 'info'],
			queryFn: () => miscApi.info(),
			enabled: isEnabled(enabled),
		}),

	info: async (): Promise<Info> => {
		return apiFetch<Info>('/info')
	},
}
