<template>
	<VContainer>
		<!-- <div class="d-flex gap-6 mb-6">
			<VImg
				v-if="content.cover_uri"
				:src="`${API_URL}/files/cover/${content.id}`"
				:aspect-ratio="2 / 3"
				width="200"
				cover
				class="rounded"
			/>
			<div>
				<h1 class="text-h4 mb-2">{{ content.title }}</h1>
				<div class="text-body-2 text-medium-emphasis">Book Series</div>
			</div>
		</div> -->

		<h2 class="text-h5 mb-4">Books</h2>
		<AContentGrid :items="children" :loading="qChildren.isLoading.value" />
	</VContainer>
</template>

<script setup lang="ts">
import AContentGrid from '@/components/AContentGrid.vue'
import { contentApi } from '@/utils/api/content'
import { computed } from 'vue'

const props = defineProps<{
	contentId: string
}>()

const qChildren = contentApi.useList(() => ({ parent_id: props.contentId }))
const children = computed(() => {
	return (qChildren.data?.value || []).slice().sort((a, b) => {
		return (a.order || 0) - (b.order || 0)
	})
})
</script>
