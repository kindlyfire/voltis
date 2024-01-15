<template>
	<div
		class="w-[250px] h-screen card p-0 border-l-0 border-t-0 border-b-0 rounded-none flex flex-col"
	>
		<div class="h-[50px] flex items-center justify-center">
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
	</div>
</template>

<script lang="ts" setup>
import ALinkButton from '../../../components/ALinkButton.vue'
import { useLibraries } from '../../../state/composables/queries'

const qLibraries = useLibraries({})
await qLibraries.suspense()
</script>

<style></style>
