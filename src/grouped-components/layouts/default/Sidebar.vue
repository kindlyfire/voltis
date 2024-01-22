<template>
	<div
		class="h-[50px] flex items-center wide:justify-center px-2 gap-1 sm:gap-2"
	>
		<UButton
			color="gray"
			class="wide:hidden"
			@click="layoutStore.sidebarOpen = false"
		>
			<UIcon name="ph:x-bold" dynamic class="h-5 scale-[1.2]" />
		</UButton>
		<NuxtLink to="/" class="font-bold text-lg font-mono">Voltis</NuxtLink>
	</div>
	<div class="p-2 flex flex-col">
		<ALinkButton to="/" size="lg">
			<Icon name="ph:house-bold" dynamic class="h-5 scale-[1.4]" />
			Home
		</ALinkButton>
		<ALinkButton to="/library" size="lg">
			<Icon name="ph:book-bookmark-bold" dynamic class="h-5 scale-[1.4]" />
			My Library
		</ALinkButton>
		<ALinkButton to="/lists" size="lg">
			<Icon name="ph:list-bullets-bold" dynamic class="h-5 scale-[1.4]" />
			My Lists
		</ALinkButton>
	</div>
</template>

<script lang="ts" setup>
import ALinkButton from '../../../components/ALinkButton.vue'
import { useDataSources } from '../../../state/composables/queries'
import { useLayoutStore } from '../state'

const qLibraries = useDataSources({})
const layoutStore = useLayoutStore()
</script>

<style></style>
