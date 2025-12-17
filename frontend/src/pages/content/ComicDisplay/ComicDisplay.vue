<template>
	<ReaderMain
		:content-id="content.id"
		:pages="pages"
		:get-page-url="getPageUrl"
		@reach-start="onReachStart"
		@reach-end="onReachEnd"
	/>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Content } from '@/utils/api/types'
import { API_URL } from '@/utils/fetch'
import type { PageInfo } from './types'
import ReaderMain from './ReaderMain.vue'

const props = defineProps<{
	content: Content
}>()

const pages = computed<PageInfo[]>(() =>
	(props.content.meta?.pages ?? []).map(([, width, height]) => ({ width, height }))
)

function getPageUrl(index: number): string {
	return `${API_URL}/files/page/${props.content.id}/${index}`
}

function onReachStart() {
	// TODO: Navigate to previous chapter
	console.log('Reached start - navigate to previous chapter')
}

function onReachEnd() {
	// TODO: Navigate to next chapter
	console.log('Reached end - navigate to next chapter')
}
</script>
