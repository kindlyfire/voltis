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
		}
	})
}

export function useItem(id: MaybeRef<string | null>) {
	return useQuery({
		queryKey: ['item', id],
		async queryFn() {
			return trpc.items.get.query({ id: unref(id)! })
		},
		enabled: computed(() => unref(id) != null)
	})
}

export function useCollection(id: MaybeRef<string | null>) {
	return useQuery({
		queryKey: ['collection', id],
		async queryFn() {
			return trpc.collections.get.query({ id: unref(id)! })
		},
		enabled: computed(() => unref(id) != null)
	})
}

export function useReaderData(id: MaybeRef<string | null>) {
	return useQuery({
		queryKey: ['reader-data', id],
		async queryFn() {
			return trpc.items.getReaderData.query({ id: unref(id)! })
		},
		enabled: computed(() => unref(id) != null)
	})
}
