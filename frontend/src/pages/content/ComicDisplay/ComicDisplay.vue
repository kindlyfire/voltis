<template>
	<ReaderMain
		:content-id="content.id"
		:pages="pages"
		:siblings="siblings"
		:get-page-url="getPageUrl"
		@reach-start="onReachStart"
		@reach-end="onReachEnd"
		@go-to-sibling="goToSibling"
	/>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import type { Content } from '@/utils/api/types'
import { contentApi } from '@/utils/api/content'
import { API_URL } from '@/utils/fetch'
import type { PageInfo, SiblingsInfo } from './types'
import ReaderMain from './ReaderMain.vue'

const props = defineProps<{
	content: Content
}>()

const router = useRouter()

const pages = computed<PageInfo[]>(() =>
	(props.content.meta?.pages ?? []).map(([, width, height]) => ({ width, height }))
)

const siblingsQuery = contentApi.useList(
	computed(() => (props.content.parent_id ? { parent_id: props.content.parent_id } : {}))
)

const siblings = computed<SiblingsInfo | null>(() => {
	if (!props.content.parent_id || !siblingsQuery.data.value) return null

	const items = [...siblingsQuery.data.value]
		.sort((a, b) => (a.order ?? 0) - (b.order ?? 0))
		.map(c => ({ id: c.id, title: c.title, order: c.order }))

	const currentIndex = items.findIndex(c => c.id === props.content.id)
	if (currentIndex === -1) return null

	return { items, currentIndex }
})

function getPageUrl(index: number): string {
	return `${API_URL}/files/page/${props.content.id}/${index}`
}

function goToSibling(id: string, fromEnd = false) {
	router.push({
		name: 'content',
		params: { id },
		query: fromEnd ? { page: 'last' } : {},
	})
}

function onReachStart() {
	const s = siblings.value
	if (s && s.currentIndex > 0) {
		const prev = s.items[s.currentIndex - 1]
		if (prev) goToSibling(prev.id, true)
	}
}

function onReachEnd() {
	const s = siblings.value
	if (s && s.currentIndex < s.items.length - 1) {
		const next = s.items[s.currentIndex + 1]
		if (next) goToSibling(next.id)
	}
}
</script>
