import { createTRPCNuxtClient, httpBatchLink } from 'trpc-nuxt/client'
import type { AppRouter } from '~/server/trpc/routers'

function createClient() {
	return createTRPCNuxtClient<AppRouter>({
		links: [
			httpBatchLink({
				url: '/api/trpc'
			})
		]
	})
}
export let trpc: ReturnType<typeof createClient>

export default defineNuxtPlugin(() => {
	trpc = createTRPCNuxtClient<AppRouter>({
		links: [
			httpBatchLink({
				url: '/api/trpc'
			})
		]
	})
})
