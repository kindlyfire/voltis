<template>
	<UButton
		v-for="link in linksExpanded"
		:key="link.to"
		:to="link.to"
		:variant="link.variant"
		size="lg"
	>
		{{ link.name }}
	</UButton>
</template>

<script lang="ts" setup>
const links = [
	{ name: 'Summary', to: '/admin/summary' },
	{ name: 'Data Sources', to: '/admin/datasources' }
]

const route = useRoute()
const linksExpanded = computed(() => {
	return links.map(link => {
		const active = route.path === link.to
		return {
			...link,
			variant: (active ? 'solid' : 'soft') as 'solid' | 'soft'
		}
	})
})
</script>

<style></style>
