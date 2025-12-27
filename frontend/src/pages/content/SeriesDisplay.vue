<template>
	<div class="top-background-wrapper">
		<div
			:style="{
				backgroundImage: content?.cover_uri
					? `url(${API_URL}/files/cover/${content.id}?v=${content.file_mtime})`
					: 'none',
			}"
			class="top-background"
		></div>
	</div>

	<VContainer class="relative xl:pt-20!">
		<div class="d-flex gap-3 md:gap-6 mb-6">
			<VCard>
				<div class="w-[100px] sm:w-[125px] md:w-[200px] shrink-0">
					<img
						v-if="content?.cover_uri"
						:src="`${API_URL}/files/cover/${content.id}`"
						class="w-full rounded aspect-2/3"
					/>
				</div>
			</VCard>
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
					<ReadingStatusButton :content-id="content?.id" />
				</div>
			</div>
		</div>

		<AContentGrid :items="children" :loading="qChildren.isLoading.value" />
	</VContainer>
</template>

<script setup lang="ts">
import AContentGrid from '@/components/AContentGrid.vue'
import { contentApi } from '@/utils/api/content'
import { API_URL } from '@/utils/fetch'
import { computed } from 'vue'
import ReadingStatusButton from './ReadingStatusButton.vue'

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
</script>

<style scoped>
/* Wrapper needed for the "overflow: hidden", so that the scaleX of the actual
background doesn't cause the page to widen. */
.top-background-wrapper {
	position: absolute;
	top: 0;
	left: 0;
	right: 0;
	height: 450px;
	overflow: hidden;
}

.top-background {
	background-size: cover;
	background-position: center;
	filter: blur(10px) brightness(0.7);
	height: 400px;
	width: 100%;
	transform: scaleX(1.1);
}
</style>
