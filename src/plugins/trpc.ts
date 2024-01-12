import { createTRPCNuxtClient, httpBatchLink } from 'trpc-nuxt/client'
import type { AppRouter } from '~/server/trpc/routers'

function createClient() {
	const headers = useRequestHeaders()
	return createTRPCNuxtClient<AppRouter>({
		links: [
			httpBatchLink({
				url: '/api/trpc',
				headers() {
					return headers
				}
			})
		]
	})
}
export let trpc: ReturnType<typeof createClient>

export default defineNuxtPlugin(() => {
	trpc = createClient()
})
