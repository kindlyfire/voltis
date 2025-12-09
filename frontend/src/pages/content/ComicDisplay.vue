<template>
	<VContainer>
		<div class="d-flex gap-6">
			<div>
				<h1 class="text-h4 mb-2">{{ content.title }}</h1>
				<div class="text-body-2 text-medium-emphasis">Comic</div>
			</div>
		</div>
	</VContainer>
</template>

<script setup lang="ts">
import type { Content } from '@/utils/api/types'

defineProps<{
	content: Content
}>()
</script>
