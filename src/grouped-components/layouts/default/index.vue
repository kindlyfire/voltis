<template>
	<div class="flex items-stretch">
		<div
			v-if="sidebarEnabled"
			class="w-[250px] shrink-0 h-screen card p-0 border-l-0 border-t-0 border-b-0 rounded-none hidden wide:flex flex-col fixed"
		>
			<Sidebar />
		</div>
		<div
			v-if="sidebarEnabled"
			class="w-[250px] shrink-0 hidden wide:block"
		></div>
		<div class="flex flex-col grow">
			<Navbar />
			<slot />
			<div class="mb-4"></div>
		</div>

		<USlideover
			side="left"
			v-model="layoutStore.sidebarOpen"
			:ui="{
				width: 'max-w-[300px]',
				wrapper: 'wide:hidden'
			}"
			:transition="false"
		>
			<Sidebar />
		</USlideover>
	</div>
</template>

<script lang="ts" setup>
import { useLayoutStore } from '../state'
import Navbar from './Navbar.vue'
import Sidebar from './Sidebar.vue'

const route = useRoute()
const sidebarEnabled = computed(() => route.meta.sidebarEnabled ?? true)
const layoutStore = useLayoutStore()
</script>

<style></style>
