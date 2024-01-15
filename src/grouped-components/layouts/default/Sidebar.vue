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
		<div class="flex flex-col">
			<div class="font-medium text-sm flex gap-x-2.5 px-3.5 py-2.5">
				<Icon name="ph:folder-notch-bold" dynamic class="h-5 scale-[1.4]" />
				Libraries
			</div>
			<ALinkButton
				v-for="lib in qLibraries.data.value"
				:to="'/library/' + lib.id"
			>
				{{ lib.name }}
			</ALinkButton>
		</div>
	</div>
</template>

<script lang="ts" setup>
import ALinkButton from '../../../components/ALinkButton.vue'
import { useLibraries } from '../../../state/composables/queries'
import { useLayoutStore } from '../state'

const qLibraries = useLibraries({})
const layoutStore = useLayoutStore()
</script>

<style></style>
