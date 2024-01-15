<template>
	<div class="flex items-stretch">
		<Sidebar v-if="sidebarEnabled" />
		<div
			id="mainOverflowArea"
			class="flex flex-col grow h-screen overflow-auto"
		>
			<Navbar />
			<slot />
			<div class="mb-4"></div>
		</div>
	</div>
</template>

<script lang="ts" setup>
import Navbar from './Navbar.vue'
import Sidebar from './Sidebar.vue'

const route = useRoute()
const sidebarEnabled = computed(() => route.meta.sidebarEnabled ?? true)
</script>

<style></style>
