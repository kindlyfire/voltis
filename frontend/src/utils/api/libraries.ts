import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { apiFetch } from '../fetch'
import type { Library, LibraryUpsert, ScanResult } from './types'

export const librariesApi = {
	useList: () =>
		useQuery({
			queryKey: ['libraries'],
			queryFn: async () => apiFetch<Library[]>('/libraries'),
		}),

	useUpsert: () => {
		const queryClient = useQueryClient()
		return useMutation({
			mutationFn: async (library: LibraryUpsert) => {
				const url = `/libraries/${library.id ?? 'new'}`
				const { id: _, ...body } = library
				return apiFetch<Library>(url, {
					method: 'POST',
					body: JSON.stringify(body),
				})
			},
			onSuccess: () => {
				queryClient.invalidateQueries({ queryKey: ['libraries'] })
			},
		})
	},

	useDelete: () => {
		const queryClient = useQueryClient()
		return useMutation({
			mutationFn: async (id: string) => apiFetch(`/libraries/${id}`, { method: 'DELETE' }),
			onSuccess: () => {
				queryClient.invalidateQueries({ queryKey: ['libraries'] })
			},
		})
	},

	useScan: () => {
		const queryClient = useQueryClient()
		return useMutation({
			mutationFn: async (opts?: { ids?: string[]; force?: boolean }) => {
				const params = new URLSearchParams()
				if (opts?.ids?.length) params.set('id', opts.ids.join(','))
				if (opts?.force) params.set('force', 'true')
				const qs = params.toString()
				return apiFetch<ScanResult[]>(`/libraries/scan${qs ? `?${qs}` : ''}`, {
					method: 'POST',
				})
			},
			onSuccess: () => {
				queryClient.invalidateQueries({ queryKey: ['libraries'] })
			},
		})
	},
}
