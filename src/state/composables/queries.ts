import { useQuery, type UseQueryOptions } from '@tanstack/vue-query'
import { trpc } from '../../plugins/trpc'
import type {
	BuildProcedure,
	inferProcedureInput,
	inferProcedureOutput
} from '@trpc/server'
import type { AppRouter } from '../../server/trpc/routers'

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

export function useMeta() {
	return useQuery({
		queryKey: ['meta'],
		async queryFn() {
			return await trpc.meta.query()
		}
	})
}

type Unref<T> = T extends Ref<infer V> ? V : T
type UseQueryWrapperOptions<T extends BuildProcedure<'query', any, any>> = Omit<
	Unref<UseQueryOptions<inferProcedureOutput<T>>>,
	'queryKey' | 'queryFn'
>
function createQueryWrapper<T extends BuildProcedure<'query', any, any>>(
	name: string,
	queryFn: (data: inferProcedureInput<T>) => Promise<inferProcedureOutput<T>>
) {
	return function wrappedQuery(
		query: MaybeRef<inferProcedureInput<T> | null>,
		options?: UseQueryWrapperOptions<T>
	) {
		return useQuery({
			...options,
			queryKey: ['trpc', name, query],
			async queryFn() {
				const q = unref(query)
				if (q == null) {
					throw new Error('query is null')
				}
				return queryFn(q)
			},
			enabled: computed(
				() => unref(options?.enabled) !== false && unref(query) != null
			)
		})
	}
}

export const useLibraries = createQueryWrapper<AppRouter['libraries']['query']>(
	'libraries',
	o => trpc.libraries.query.query(o)
)

export const useCollections = createQueryWrapper<
	AppRouter['collections']['query']
>('collections', o => trpc.collections.query.query(o))

const _useCollection = createQueryWrapper<AppRouter['collections']['get']>(
	'collection',
	o => trpc.collections.get.query(o)
)
export const useCollection = (id: MaybeRef<string | null | undefined>) =>
	_useCollection(computed(() => (unref(id) ? { id: unref(id)! } : null)))

export const useItems = createQueryWrapper<AppRouter['items']['query']>(
	'items',
	o => trpc.items.query.query(o)
)
const _useItem = createQueryWrapper<AppRouter['items']['get']>('item', o =>
	trpc.items.get.query(o)
)
export const useItem = (id: MaybeRef<string | null | undefined>) =>
	_useItem(computed(() => (unref(id) ? { id: unref(id)! } : null)))
