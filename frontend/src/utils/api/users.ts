import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { apiFetch, RequestError } from '../fetch'
import type { User, UserUpsert } from './types'

export const usersApi = {
	useList: () =>
		useQuery({
			queryKey: ['users'],
			queryFn: async () => apiFetch<User[]>('/users'),
		}),

	useMe: () =>
		useQuery({
			queryKey: ['users', 'me'],
			queryFn: async () =>
				apiFetch<User>('/users/me').catch(e => {
					if (e instanceof RequestError && e.response?.status === 401) {
						return null
					}
					throw e
				}),
		}),

	useUpsert: () => {
		const queryClient = useQueryClient()
		return useMutation({
			mutationFn: async (user: UserUpsert) => {
				const url = `/users/${user.id ?? 'new'}`
				const { id: _, ...body } = user
				return apiFetch<User>(url, {
					method: 'POST',
					body: JSON.stringify(body),
				})
			},
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
