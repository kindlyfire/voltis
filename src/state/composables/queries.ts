import { useQuery } from '@tanstack/vue-query'
import { trpc } from '../../plugins/trpc'

export function useUser() {
	return useQuery({
		queryKey: ['user'],
		async queryFn() {
			try {
				return await trpc.auth.me.query()
			} catch (e) {
				return null
			}
		}
	})
}

export function useLibraries() {
	return useQuery({
		queryKey: ['libraries'],
		async queryFn() {
			return await trpc.libraries.query.query({})
		},
		enabled: process.client
	})
}
