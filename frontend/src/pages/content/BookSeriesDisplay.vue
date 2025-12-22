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
		<div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-8 gap-4">
			<RouterLink
				v-for="item in children"
				:key="item.id"
				:to="`/${item.id}`"
				class="block"
				:title="item.title"
			>
				<VCard>
					<VImg
						v-if="item.cover_uri"
						:src="`${API_URL}/files/cover/${item.id}?v=${item.file_mtime}`"
						:aspect-ratio="2 / 3"
						cover
					/>
					<VCardTitle class="text-body-2">{{ item.title }}</VCardTitle>
				</VCard>
			</RouterLink>
		</div>
	</VContainer>
</template>

<script setup lang="ts">
import { contentApi } from '@/utils/api/content'
import { API_URL } from '@/utils/fetch'
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
