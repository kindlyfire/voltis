<template>
	<div ref="imageWrapperRef" class="cursor-pointer flex flex-col items-center">
		<template v-for="p of pages">
			<div
				class="max-w-full"
				:style="{
					width: `${p.page.width}px`,
					aspectRatio: `${p.page.width} / ${p.page.height}`
				}"
			>
				<div
					class="h-[400px] w-full max-h-full items-center justify-center"
					:class="[!p.blobUrl || p.error ? 'flex' : 'hidden']"
				>
					<template v-if="p.error">
						<div>
							{{ p.error }}
						</div>
						<div>
							<UButton @click="p.fetch">Retry</UButton>
						</div>
					</template>
					<div v-else>
						<UIcon
							name="ph:circle-dashed-bold"
							dynamic
							class="h-10 w-10 animate-spin"
						/>
					</div>
				</div>
				<img
					:class="[!p.blobUrl || p.error ? 'hidden' : '']"
					:src="p.blobUrl || ''"
				/>
			</div>
		</template>
	</div>
	<div class="fixed bottom-0 pointer-events-none" ref="screenBottomRef"></div>
</template>

<script lang="ts" setup>
import { useScroll, watchDebounced } from '@vueuse/core'
import {
	SwitchChapterDirection,
	SwitchChapterPagePosition,
	readerKey
} from './use-reader'
import { useReaderActions } from './use-reader-actions'
import {
	getPagesInPreloadOrder,
	preloadPages,
	type PageLoader
} from './page-loader'

const reader = inject(readerKey)!
const imageWrapperRef = ref<HTMLDivElement | null>(null)
const screenBottomRef = ref<HTMLDivElement | null>(null)

const chapterPages = computed(() => {
	return reader.state.chaptersPages.get(reader.state.chapterId)
})
const pages = chapterPages

const getScreenHeight = () =>
	screenBottomRef.value?.getBoundingClientRect().y ?? document.body.clientHeight
useReaderActions({
	onBack() {
		const screenHeight = getScreenHeight()
		const el = reader.state.scrollRef!

		if (el.scrollTop === 0) {
			reader.switchChapter(
				SwitchChapterDirection.Backward,
				SwitchChapterPagePosition.End
			)
		} else {
			// Scroll up/down by 95% of the screen height
			el.scrollTo({
				top: el.scrollTop + -0.95 * screenHeight,
				left: 0,
				behavior: 'smooth'
			})
		}
	},
	onNext() {
		const screenHeight = getScreenHeight()
		const el = reader.state.scrollRef!

		if (el.scrollTop + el.clientHeight > el.scrollHeight - 10) {
			reader.switchChapter(SwitchChapterDirection.Forward)
		} else {
			// Scroll up/down by 95% of the screen height
			el.scrollTo({
				top: el.scrollTop + 0.95 * screenHeight,
				left: 0,
				behavior: 'smooth'
			})
		}
	}
})

const averagePageHeight = computed(() => {
	if (!chapterPages.value?.length) return null
	let sum = 0
	for (const page of chapterPages.value) {
		sum += page.page.height
	}
	return sum / chapterPages.value.length
})

// Page loading strategy
watchEffect(() => {
	if (!chapterPages.value) return
	const pagesInPreloadOrder = getPagesInPreloadOrder(
		chapterPages.value,
		reader.state.page
	)
	const pagesPerScreen = Math.ceil(
		reader.state.scrollRef!.clientHeight / averagePageHeight.value!
	)
	const pagesToPreload = pagesPerScreen * 5
	const preloadConcurrency = pagesPerScreen * 2
	preloadPages(pagesInPreloadOrder.slice(0, pagesToPreload), preloadConcurrency)
})

const pageScroll = useScroll(reader.state.scrollRef!)
function getViewingPage() {
	const children = Array.from(
		imageWrapperRef.value!.children
	) as HTMLDivElement[]
	const lastChildInViewport = children.findLast(el => {
		return el.offsetTop < pageScroll.y.value + window.innerHeight
	})
	return lastChildInViewport ? children.indexOf(lastChildInViewport) : null
}

// Sync back page index as the user scrolls
// Automatically update page index in longstrip mode based on scroll
watchDebounced(
	() => pageScroll.y.value,
	() => {
		const viewingPage = getViewingPage()
		if (viewingPage != null) {
			reader.setPageTo(viewingPage)
		}
	},
	{ debounce: 25 }
)

function scrollToPage() {
	const el = imageWrapperRef.value?.children[
		reader.state.page
	] as HTMLDivElement
	if (!el) return
	reader.state.scrollRef?.scrollTo({
		top: el.offsetTop,
		behavior: 'smooth'
	})
}

watch(
	() => chapterPages.value,
	(v, oldV) => {
		if (v && v !== oldV) scrollToPage()
	}
)
onMounted(() => {
	scrollToPage()
})
onUnmounted(
	reader.hooks.hook('goToPage', page => {
		scrollToPage()
	})
)
</script>

<style></style>
