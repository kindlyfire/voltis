<template>
	<Page />
</template>

<script lang="ts" setup>
import Page from '@/grouped-components/pages/read/[itemId]/page.vue'

definePageMeta({
	sidebarEnabled: false,

	// Constant key needed to prevent the entire page from being
	// re-rendered from scratch when navigating between items
	key: '/read/[itemId]/[page]'
})
</script>

<style></style>
