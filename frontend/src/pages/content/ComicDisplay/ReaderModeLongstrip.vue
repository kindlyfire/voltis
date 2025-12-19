<template>
	<div ref="containerRef" class="reader-longstrip d-flex flex-column align-center">
		<template v-for="(loader, index) in reader.loaders" :key="index">
			<div class="reader-longstrip__page" :style="getPageStyle(index)">
				<div
					v-if="loader.error.value"
					class="reader-longstrip__placeholder d-flex flex-column align-center justify-center gap-2"
				>
					<div class="text-error">{{ loader.error.value }}</div>
					<VBtn size="small" @click="loader.load()">Retry</VBtn>
				</div>
				<div
					v-else-if="loader.loading.value || !loader.blobUrl.value"
					class="reader-longstrip__placeholder d-flex align-center justify-center"
				>
					<VProgressCircular indeterminate size="32" />
				</div>
				<img v-else :src="loader.blobUrl.value" class="reader-longstrip__image" />
			</div>
		</template>
	</div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, watchEffect } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import { useReaderStore } from './use-reader-store'

const reader = useReaderStore()
const containerRef = ref<HTMLElement | null>(null)

function getPageStyle(index: number) {
	const page = reader.pages[index]
	if (!page) return {}
	return {
		width: `min(100%, ${page.width}px)`,
		aspectRatio: `${page.width} / ${page.height}`,
	}
}

// Update current page based on scroll position
const updateCurrentPage = useDebounceFn(
	() => {
		if (!containerRef.value) return

		const container = containerRef.value
		const children = Array.from(container.children) as HTMLElement[]
		const viewportCenter = window.scrollY + window.innerHeight / 2

		// Find page at center of viewport
		for (let i = children.length - 1; i >= 0; i--) {
			const el = children[i]!
			if (el.offsetTop <= viewportCenter) {
				if (reader.currentPage !== i) {
					reader.currentPage = i
				}
				break
			}
		}
	},
	50,
	{ maxWait: 100 }
)

onMounted(() => {
	window.addEventListener('scroll', updateCurrentPage)
})

onUnmounted(() => {
	window.removeEventListener('scroll', updateCurrentPage)
})

// Scroll to initial page on mount
watchEffect(
	() => {
		if (!containerRef.value) return
		const children = Array.from(containerRef.value.children) as HTMLElement[]
		const target = children[reader.currentPage]
		if (target) {
			target.scrollIntoView({ behavior: 'instant', block: 'start' })
		}
	},
	{ flush: 'post' }
)
</script>

<style scoped>
.reader-longstrip {
	width: 100%;
	min-height: calc(100dvh - var(--v-layout-top, 0px));
}

.reader-longstrip__page {
	max-width: 100%;
}

.reader-longstrip__placeholder {
	width: 100%;
	height: 100%;
	min-height: 200px;
	background: rgba(128, 128, 128, 0.1);
}

.reader-longstrip__image {
	width: 100%;
	height: auto;
	display: block;
}
</style>
