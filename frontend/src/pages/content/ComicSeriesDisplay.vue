<template>
	<VContainer>
		<div class="d-flex gap-6 mb-6">
			<div class="w-[225px] shrink-0">
				<img
					v-if="content.cover_uri"
					:src="`${API_URL}/files/cover/${content.id}`"
					class="w-full rounded aspect-2/3"
				/>
			</div>
			<div>
				<h1 class="text-h4 mb-2">{{ content.title }}</h1>
				<div class="text-body-2 text-medium-emphasis">Comic Series</div>
			</div>
		</div>

		<h2 class="text-h5 mb-4">Issues</h2>
		<div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-8 gap-4">
			<RouterLink
				v-for="item in children.data?.value"
				:key="item.id"
				:to="`/${item.id}`"
				class="block"
			>
				<VCard>
					<VImg
						v-if="item.cover_uri"
						:src="`${API_URL}/files/cover/${item.id}`"
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
import { computed } from 'vue'
import type { Content } from '@/utils/api/types'
import { contentApi } from '@/utils/api/content'
import { API_URL } from '@/utils/fetch'

const props = defineProps<{
	content: Content
}>()

const children = contentApi.useList(computed(() => ({ parent_id: props.content.id })))
</script>
