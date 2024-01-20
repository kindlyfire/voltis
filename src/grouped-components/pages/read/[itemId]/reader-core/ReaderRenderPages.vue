<template>
	<div
		class="h-screen w-screen shrink-0 flex flex-row items-center justify-center cursor-pointer"
	>
		<!-- Prevents scroll jumping up when going from an image that is wider
		than the screen to an image that is taller than the screen. -->
		<div class="h-screen w-[1px] -ml-[1px]"></div>
		<div v-if="!chapterPages || !p">Loading...</div>
		<div
			v-else-if="p.error || !p.blobUrl"
			class="flex flex-col items-center justify-center gap-2"
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
		<img v-else :src="p.blobUrl" class="max-h-full max-w-full" />
	</div>
</template>

<script lang="ts" setup>
import { getPagesInPreloadOrder, preloadPages } from './page-loader'
import { SwitchChapterDirection, SwitchChapterPagePosition } from './types'
import { readerKey } from './use-reader'
import { useReaderActions } from './use-reader-actions'

const reader = inject(readerKey)!

const chapterPages = computed(() => {
	return reader.state.chaptersPages.get(reader.state.chapterId)
})
const p = computed(() => {
	if (!chapterPages.value) return
	const p = chapterPages.value[reader.state.page]
	if (!p) return
	p.fetch()
	return p
})

useReaderActions({
	onBack() {
		if (!chapterPages.value) return
		if (reader.state.page === 0)
			return reader.switchChapter(
				SwitchChapterDirection.Backward,
				SwitchChapterPagePosition.End
			)
		reader.setPageTo(reader.state.page - 1)
	},
	onNext() {
		if (!chapterPages.value) return
		if (reader.state.page >= chapterPages.value.length - 1)
			return reader.switchChapter(SwitchChapterDirection.Forward)
		reader.setPageTo(reader.state.page + 1)
	}
})

// Page loading strategy
watchEffect(() => {
	if (!chapterPages.value) return
	const pagesInPreloadOrder = getPagesInPreloadOrder(
		chapterPages.value,
		reader.state.page
	)
	const pagesToPreload = 5
	const preloadConcurrency = 2
	preloadPages(pagesInPreloadOrder.slice(0, pagesToPreload), preloadConcurrency)
})

onMounted(() => {
	reader.state.scrollRef?.scrollTo({
		top: reader.state.mainRef?.offsetTop,
		behavior: 'smooth'
	})
})
</script>

<style></style>
