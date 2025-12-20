import { useQuery } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { apiFetch } from '../fetch'
import type { Content, ContentListParams } from './types'

export const contentApi = {
	useGet: (id: MaybeRefOrGetter<string>) =>
		useQuery({
			queryKey: ['content', id],
			queryFn: async () => apiFetch<Content>(`/content/${toValue(id)}`),
		}),

	useList: (params: MaybeRefOrGetter<ContentListParams> = {}) =>
		useQuery({
			queryKey: ['content', 'list', params],
			queryFn: async () => {
				const p = toValue(params)
				const searchParams = new URLSearchParams()
				if (p.parent_id) searchParams.append('parent_id', p.parent_id)
				if (p.library_id) searchParams.append('library_id', p.library_id)
				if (p.type) {
					for (const t of p.type) {
						searchParams.append('type', t)
					}
				}
				if (p.valid !== undefined) searchParams.append('valid', String(p.valid))

				const query = searchParams.toString()
				return apiFetch<Content[]>(`/content${query ? `?${query}` : ''}`)
			},
		}),
}
