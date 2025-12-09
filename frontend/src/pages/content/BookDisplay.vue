<template>
	<VContainer>
		<div class="d-flex gap-6">
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
				<div class="text-body-2 text-medium-emphasis">Book</div>
			</div>
		</div>
	</VContainer>
</template>

<script setup lang="ts">
import type { Content } from '@/utils/api/types'
import { API_URL } from '@/utils/fetch'

defineProps<{
	content: Content
}>()
</script>