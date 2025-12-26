<template>
	<RouterLink :to="to" class="block" :title="title">
		<VCard class="relative aspect-2/3">
			<VImg v-if="coverUri" :src="coverUri" :aspect-ratio="2 / 3" cover />

			<span
				v-if="childrenCount != null"
				class="absolute top-2 right-2 bg-black/70 text-white text-xs font-medium px-2 py-0.5 rounded-full"
			>
				{{ childrenCount }}
			</span>
		</VCard>

		<div class="text-body-2 pt-2">{{ title }}</div>
	</RouterLink>
</template>

<script setup lang="ts">
const props = defineProps<{
	to: string
	title: string
	coverUri: string | null
	childrenCount?: number | null
}>()
</script>
