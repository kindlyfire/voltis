<template>
	<div
		:style="{
			backgroundImage: content?.cover_uri
				? `url(${API_URL}/files/cover/${content.id}?v=${content.file_mtime})`
				: 'none',
			backgroundSize: 'cover',
			backgroundPosition: 'center',
			filter: 'blur(10px) brightness(0.7)',
			height: '400px',
			position: 'absolute',
			top: '-24px',
			left: '-24px',
			right: '-24px',
		}"
	></div>

	<VContainer class="relative xl:pt-20!">
		<div class="d-flex gap-3 md:gap-6 mb-6">
			<div class="w-[100px] sm:w-[125px] md:w-[200px] shrink-0">
				<img
					v-if="content?.cover_uri"
					:src="`${API_URL}/files/cover/${content.id}`"
					class="w-full rounded aspect-2/3"
				/>
			</div>
			<div class="space-y-2!">
				<h1
					class="text-xl sm:text-2xl md:text-3xl xl:text-5xl font-bold! text-shadow-md/40! text-white!"
				>
					{{ content?.title }}
				</h1>
				<div class="text-shadow-md/40! text-white!">
					<template v-if="content?.type === 'comic_series'">Comic Series</template>
					<template v-else-if="content?.type === 'book_series'">Book Series</template>
				</div>
				<div>
					<VSelect
						:model-value="currentStatus"
						:items="statusOptions"
						:loading="qContent.isLoading.value || mUpdateUserData.isPending.value"
						:placeholder="currentStatus ? undefined : 'Add to Library'"
						density="comfortable"
						variant="solo"
						hide-details
						clearable
						@update:model-value="updateStatus"
						class="mt-4 max-w-60"
					/>
				</div>
			</div>
		</div>

		<AContentGrid :items="children" :loading="qChildren.isLoading.value" />
	</VContainer>
</template>

<script setup lang="ts">
import AContentGrid from '@/components/AContentGrid.vue'
import { contentApi } from '@/utils/api/content'
import type { ReadingStatus } from '@/utils/api/types'
import { API_URL } from '@/utils/fetch'
import { computed, ref } from 'vue'

const props = defineProps<{
	contentId: string
}>()

const qContent = contentApi.useGet(() => props.contentId)
const content = qContent.data

const qChildren = contentApi.useList(() => ({ parent_id: props.contentId }))
const children = computed(() => {
	return (qChildren.data?.value || []).slice().sort((a, b) => {
		return (a.order || 0) - (b.order || 0)
	})
})

const statusOptions: { value: ReadingStatus; title: string }[] = [
	{ value: 'reading', title: 'Reading' },
	{ value: 'completed', title: 'Completed' },
	{ value: 'on_hold', title: 'On Hold' },
	{ value: 'dropped', title: 'Dropped' },
	{ value: 'plan_to_read', title: 'Plan to Read' },
]

const currentStatus = computed(() => content.value?.user_data?.status ?? null)
const mUpdateUserData = contentApi.useUpdateUserData()

async function updateStatus(status: ReadingStatus | null) {
	if (!content.value) return
	await mUpdateUserData.mutateAsync({ contentId: content.value.id, status })
	await qContent.refetch()
}
</script>
