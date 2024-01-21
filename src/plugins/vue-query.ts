import {
	QueryClient,
	VueQueryPlugin,
	type DehydratedState,
	dehydrate,
	hydrate
} from '@tanstack/vue-query'

export default defineNuxtPlugin(nuxtApp => {
	const vueQueryState = useState<DehydratedState | null>('vue-query')

	const queryClient = new QueryClient({
		defaultOptions: {
			queries: {
				retry: false
			}
		}
	})

	nuxtApp.vueApp.use(VueQueryPlugin, { queryClient })

	if (process.server) {
		nuxtApp.hooks.hook('app:rendered', () => {
			vueQueryState.value = dehydrate(queryClient)
		})
	}

	if (process.client) {
		nuxtApp.hooks.hook('app:created', () => {
			hydrate(queryClient, vueQueryState.value)
		})
	}
})
