export const useLayoutStore = defineStore('layout', () => {
	const sidebarOpen = ref(false)
	const route = useRoute()
	watch(
		() => route.path,
		() => {
			sidebarOpen.value = false
		}
	)

	return {
		sidebarOpen,
		toggleSidebar() {
			sidebarOpen.value = !sidebarOpen.value
		}
	}
})

if (import.meta.hot)
	import.meta.hot.accept(acceptHMRUpdate(useLayoutStore, import.meta.hot))
