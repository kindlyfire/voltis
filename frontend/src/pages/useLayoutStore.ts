import { acceptHMRUpdate, defineStore } from 'pinia'
import { onBeforeMount, onUnmounted, ref } from 'vue'

export const useLayoutStore = defineStore('layout', () => {
	return {
		staticNavbar: ref(false),
	}
})

if (import.meta.hot) {
	import.meta.hot.accept(acceptHMRUpdate(useLayoutStore, import.meta.hot))
}

export function useStaticNavbar() {
	const store = useLayoutStore()

	onBeforeMount(() => {
		store.staticNavbar = true
	})

	onUnmounted(() => {
		store.staticNavbar = false
	})
}
