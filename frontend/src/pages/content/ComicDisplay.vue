<template>
	<VContainer>
		<div class="d-flex gap-6 mb-6">
			<div>
				<h1 class="text-h4 mb-2">{{ content.title }}</h1>
				<div class="text-body-2 text-medium-emphasis">Comic</div>
			</div>
		</div>
		<div class="d-flex flex-column align-center gap-2">
			<img
				v-for="(_, index) in pages"
				:key="index"
				:src="`${API_URL}/files/page/${content.id}/${index}`"
				class="max-w-full"
			/>
		</div>
	</VContainer>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Content } from '@/utils/api/types'
import { API_URL } from '@/utils/fetch'

const props = defineProps<{
	content: Content
}>()

const pages = computed(() => (props.content.metadata_ as { pages?: string[] })?.pages ?? [])
</script>
