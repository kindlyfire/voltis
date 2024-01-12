import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'

export const queryClient = new QueryClient({})

export default defineNuxtPlugin(nuxtApp => {
	nuxtApp.vueApp.use(VueQueryPlugin, {
		queryClient
	})
})
