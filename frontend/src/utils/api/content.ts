import { useMutation, useQuery, type UseQueryOptions } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter, type UnwrapRef } from 'vue'
import { API_URL, apiFetch } from '../fetch'
import type {
	BookChapter,
	Content,
	ContentListParams,
	UserToContent,
	UserToContentUpdate,
} from './types'
import { isEnabled } from './_utils'

export const contentApi = {
	useGet: (id: MaybeRefOrGetter<string | undefined | null>) =>
		useQuery({
			queryKey: ['content', id],
			queryFn: async () => contentApi.get(toValue(id)!),
			enabled: isEnabled(id),
		}),

	get: async (id: string) => {
		return apiFetch<Content>(`/content/${id}`)
	},

	useList: (
		params: MaybeRefOrGetter<ContentListParams | undefined> = {},
		options?: Omit<UnwrapRef<UseQueryOptions<Content[]>>, 'queryKey' | 'queryFn'>
	) =>
		useQuery({
			queryKey: ['content', 'list', params],
			queryFn: async () => {
				const p = toValue(params)!
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
				const items = await apiFetch<Content[]>(`/content${query ? `?${query}` : ''}`)
				if (p.sort) {
					return items.sort((a, b) => {
						return (a.order || 0) - (b.order || 0)
					})
				} else {
					return items
				}
			},
			enabled: isEnabled(params),
			...options,
		}),

	useBookChapters: (id: MaybeRefOrGetter<string | undefined | null>) =>
		useQuery({
			queryKey: ['content', 'book-chapters', id],
			queryFn: async () => apiFetch<BookChapter[]>(`/files/book-chapters/${toValue(id)}`),
			enabled: isEnabled(id),
		}),

	useBookChapter: (
		id: MaybeRefOrGetter<string | undefined | null>,
		href: MaybeRefOrGetter<string | undefined | null>
	) =>
		useQuery({
			queryKey: ['content', 'book-chapter', id, href],
			queryFn: async () => {
				const params = new URLSearchParams({ href: toValue(href)! })
				const res = await fetch(`${API_URL}/files/book-chapter/${toValue(id)}?${params}`, {
					credentials: 'include',
				})
				if (!res.ok) throw new Error('Failed to fetch chapter')
				return res.text()
			},
			enabled: isEnabled([id, href]),
		}),

	useUpdateUserData: () =>
		useMutation({
			mutationFn: (data: UserToContentUpdate & { contentId: string }) =>
				contentApi.updateUserData(data.contentId, data),
		}),

	updateUserData: async (
		contentId: string,
		data: UserToContentUpdate
	): Promise<UserToContent> => {
		return apiFetch<UserToContent>(`/content/${contentId}/user-data`, {
			method: 'POST',
			body: JSON.stringify(data),
		})
	},

	resetSeriesProgress: async (contentId: string): Promise<void> => {
		await apiFetch(`/content/${contentId}/reset-series-progress`, {
			method: 'POST',
		})
	},
}
