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
import { ref, onMounted, onUnmounted, watch } from 'vue'
import { useDebounceFn } from '@vueuse/core'
import { SetPage, useReaderStore } from './use-reader-store'
import { getScrollParent } from '@/utils/css'

const reader = useReaderStore()
const containerRef = ref<HTMLElement | null>(null)

function getPageStyle(index: number) {
	const page = reader.pages[index]
	if (!page) return {}
	const widthPercent = reader.settings.longstripWidth
	return {
		width: `min(${widthPercent}%, ${page.width}px)`,
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
					reader.setCurrentPage(i, SetPage.BACKGROUND)
				}
				break
			}
		}
	},
	50,
	{ maxWait: 150 }
)

function scrollToPage(index: number, instant: boolean) {
	if (!containerRef.value) return

	const container = containerRef.value
	const children = Array.from(container.children) as HTMLElement[]
	const target = children[index]
	if (target) {
		const layoutTop = parseInt(
			getComputedStyle(document.documentElement).getPropertyValue('--v-layout-top') || '0'
		)
		const scrollParent = getScrollParent(container)
		if (index === reader.pages.length - 1) {
			// Scroll to bottom for last page
			;(scrollParent || window).scrollTo({
				top: document.body.scrollHeight,
				behavior: instant ? 'instant' : 'smooth',
			})
		} else {
			;(scrollParent || window).scrollTo({
				top: target.offsetTop - layoutTop,
				behavior: instant ? 'instant' : 'smooth',
			})
		}
	}
}

onMounted(() => {
	window.addEventListener('scroll', updateCurrentPage)
	reader.setOnScrollToPageFn(scrollToPage)

	scrollToPage(reader.currentPage, true)
})

onUnmounted(() => {
	window.removeEventListener('scroll', updateCurrentPage)
	reader.setOnScrollToPageFn(undefined)
})

watch(
	() => containerRef.value,
	newVal => {
		if (newVal) reader.scrollRef = getScrollParent(newVal)
	},
	{ immediate: true }
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
