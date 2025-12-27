<template>
	<RouterLink :to="to" class="block" :title="title">
		<VCard class="relative">
			<img
				v-if="coverUri"
				:src="coverUri"
				:style="{
					aspectRatio: '2 / 3',
					objectFit: 'cover',
					width: '100%',
				}"
			/>

			<span
				v-if="userData?.status"
				class="absolute top-2 left-2 bg-black/70 text-white p-1 rounded-full aspect-square w-[18px] flex items-center justify-center"
				:title="`Status: ${STATUS_TITLES[userData.status]}`"
			>
				<VIcon :icon="statusIcon" size="12" />
			</span>

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
import type { ReadingStatus, UserToContent } from '@/utils/api/types'
import { computed } from 'vue'

const props = defineProps<{
	id: string
	to: string
	title: string
	coverUri: string | null
	childrenCount?: number | null
	userData?: UserToContent | null
}>()

const STATUS_ICONS: Record<ReadingStatus, string> = {
	reading: 'mdi-book-open-page-variant',
	completed: 'mdi-check',
	on_hold: 'mdi-pause',
	dropped: 'mdi-close',
	plan_to_read: 'mdi-bookmark',
}

const STATUS_TITLES: Record<ReadingStatus, string> = {
	reading: 'Reading',
	completed: 'Completed',
	on_hold: 'On Hold',
	dropped: 'Dropped',
	plan_to_read: 'Plan to Read',
}

const statusIcon = computed(() => {
	if (!props.userData?.status) return
	return STATUS_ICONS[props.userData.status]
})
</script>
