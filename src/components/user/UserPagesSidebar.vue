<template>
	<div class="w-[300px] shrink-0 flex flex-col gap-2">
		<UButton
			v-for="link in linksExpanded"
			:key="link.to"
			:to="link.to"
			:variant="link.variant"
			size="lg"
		>
			{{ link.name }}
		</UButton>
	</div>
</template>

<script lang="ts" setup>
const links = [
	{ name: 'My account', to: '/user/account' },
	{ name: 'Preferences', to: '/user/preferences' }
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
