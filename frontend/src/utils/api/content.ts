import { useMutation, useQuery, type UseQueryOptions } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter, type UnwrapRef } from 'vue'
import { API_URL, apiFetch } from '../fetch'
import type {
	BookChapter,
	Content,
	ContentListParams,
	Paginated,
	UserToContent,
	UserToContentUpdate,
} from './types'
import { isEnabled } from './_utils'

type QueryOptions<T> = Omit<UnwrapRef<UseQueryOptions<T>>, 'queryKey' | 'queryFn'>

export const contentApi = {
	useGet: (
		id: MaybeRefOrGetter<string | undefined | null>,
		options: QueryOptions<Content> = {}
	) =>
		useQuery({
			queryKey: ['content', id],
			queryFn: async () => contentApi.get(toValue(id)!),
			enabled: isEnabled(id),
			...options,
		}),

	get: async (id: string) => {
		return apiFetch<Content>(`/content/${id}`)
	},

	useList: (
		params: MaybeRefOrGetter<ContentListParams | undefined> = {},
		options: QueryOptions<Paginated<Content>> = {}
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
				if (p.reading_status) searchParams.append('reading_status', p.reading_status)
				if (p.limit !== undefined) searchParams.append('limit', String(p.limit))
				if (p.offset !== undefined) searchParams.append('offset', String(p.offset))
				if (p.sort) searchParams.append('sort', p.sort)
				if (p.sort_order) searchParams.append('sort_order', p.sort_order)

				const query = searchParams.toString()
				return apiFetch<Paginated<Content>>(`/content${query ? `?${query}` : ''}`)
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
