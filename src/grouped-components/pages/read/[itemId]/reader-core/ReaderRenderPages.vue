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
import {
	SwitchChapterDirection,
	SwitchChapterPagePosition,
	readerKey
} from './use-reader'
import { useReaderActions } from './use-reader-actions'

const reader = inject(readerKey)!

const chapterPages = computed(() => {
	return reader.state.chaptersPages.get(reader.state.chapterId)
})
const p = computed(() => {
	if (!chapterPages.value) return
	const p = chapterPages.value[reader.state.page]
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
		reader.state.page--
		reader.state.provider.onPageChange(reader.state.page)
	},
	onNext() {
		if (!chapterPages.value) return
		if (reader.state.page >= chapterPages.value.length - 1)
			return reader.switchChapter(SwitchChapterDirection.Forward)
		reader.state.page++
		reader.state.provider.onPageChange(reader.state.page)
	}
})

// Page loading strategy
watchEffect(() => {
	const pageIndex = reader.state.page

	if (!chapterPages.value) return
	const pagesToPreload = chapterPages.value.slice(pageIndex, pageIndex + 3)

	// We make sure the first page is loaded, then the second page, and then the
	// third and fourth can be loaded in parallel
	pagesToPreload[0].fetch()
	if (!pagesToPreload[0].blobUrl || pagesToPreload.length < 2) return
	pagesToPreload[1].fetch()
	if (!pagesToPreload[1].blobUrl) return
	pagesToPreload[2]?.fetch()
	pagesToPreload[3]?.fetch()
})

onMounted(() => {
	reader.state.scrollRef?.scrollTo({
		top: reader.state.mainRef?.offsetTop,
		behavior: 'smooth'
	})
})
</script>

<style></style>
