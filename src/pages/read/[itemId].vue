<template>
	<Page />
</template>

<script lang="ts" setup>
import Page from '@/grouped-components/pages/read/[itemId]/page.vue'

definePageMeta({
	sidebarEnabled: false,

	// Constant key and keepalive needed to prevent the entire page from being
	// re-rendered from scratch when navigating between items
	keepalive: true,
	key: '/read/[itemId]'
})
</script>

<style></style>
