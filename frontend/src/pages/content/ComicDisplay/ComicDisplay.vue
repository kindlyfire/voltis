<template>
	<ReaderMain
		:content-id="contentId"
		:getPageImageUrl="getPageUrl"
		@reach-start="onReachStart"
		@reach-end="onReachEnd"
		@go-to-sibling="goToSibling"
	/>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import { API_URL } from '@/utils/fetch'
import ReaderMain from './ReaderMain.vue'
import { useReaderStore, type ReaderStore } from './useComicDisplayStore'

const props = defineProps<{
	contentId: string
}>()

const reader = useReaderStore()
const router = useRouter()

function getPageUrl(index: number): string {
	return `${API_URL}/files/comic-page/${props.contentId}/${index}?v=${reader.content?.file_mtime ?? ''}`
}

function goToSibling(id: string, fromEnd = false) {
	router.push({
		name: 'content',
		params: { id },
		query: fromEnd ? { page: 'last' } : {},
	})
}

function onReachStart(reader: ReaderStore) {
	const s = reader.siblings
	if (s && s.currentIndex > 0) {
		const prev = s.items[s.currentIndex - 1]
		if (prev) goToSibling(prev.id, true)
	}
}

function onReachEnd(reader: ReaderStore) {
	const s = reader.siblings
	if (s && s.currentIndex < s.items.length - 1) {
		const next = s.items[s.currentIndex + 1]
		if (next) goToSibling(next.id)
	}
}
</script>
