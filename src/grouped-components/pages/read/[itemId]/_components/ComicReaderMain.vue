<template>
	<div
		class="-mb-4 mt-4 relative"
		:class="modeClasses[store.readerMode.value].main"
		ref="readerMainRef"
		@click="onReaderClick"
	>
		<ComicReaderPages />
	</div>
	<div class="absolute bottom-0 inset-x-0 pointer-events-none p-1 pt-0">
		<UProgress :value="progress" size="sm" />
	</div>
</template>

<script lang="ts" setup>
import { useScroll, watchDebounced } from '@vueuse/core'
import { readerStateKey } from '../state'
import ComicReaderPages from './ComicReaderPages.vue'
import { modeClasses } from './shared'

const store = inject(readerStateKey)!

const readerMainRef = ref<HTMLDivElement | null>(null)
const mainOverflowArea = ref<HTMLElement | null>(null)
onMounted(() => {
	mainOverflowArea.value = document.getElementById('mainOverflowArea')
})

defineExpose({
	readerMainRef
})

const progress = computed(() => {
	return (
		((store.readerState.pageIndex + 1) / store.readerState.pages.length) * 100
	)
})

// Automatically update page index in longstrip mode based on scroll
const pageScroll = useScroll(mainOverflowArea)
watchDebounced(
	() => pageScroll.y.value,
	() => {
		if (store.readerMode.value === 'longstrip') {
			const children = Array.from(
				readerMainRef.value!.children
			) as HTMLImageElement[]
			const lastChildInViewport = children.findLast(el => {
				return el.offsetTop < pageScroll.y.value + window.innerHeight
			})
			if (lastChildInViewport)
				store.readerState.pageIndex = children.indexOf(lastChildInViewport)
		}
	},
	{ debounce: 25 }
)

enum ReaderAction {
	Menu,
	Previous,
	Next
}
function onReaderClick(ev: MouseEvent) {
	// Calculate which zone the click was in (left, center, right) and resolve
	// it to an action
	const width = window.innerWidth
	const centerWidth = Math.min(width / 3, 150)
	const firstZoneOffset = width / 2 - centerWidth / 2
	const secondZoneOffset = firstZoneOffset + centerWidth
	let action: ReaderAction = ReaderAction.Menu
	if (ev.clientX < firstZoneOffset) {
		action = ReaderAction.Previous
	} else if (ev.clientX > secondZoneOffset) {
		action = ReaderAction.Next
	}

	if (action === ReaderAction.Menu) {
		// TODO
		return
	}

	if (store.readerMode.value === 'pages') {
		store.switchPage(action === ReaderAction.Previous ? -1 : 1)
	} else {
		// If at top, go to previous chapter. If at bottom, go to next chapter.
		// Otherwise scroll up/down
		const el = mainOverflowArea.value!
		if (el.scrollTop === 0 && action === ReaderAction.Previous) {
			store.switchChapter(-1)
		} else if (
			el.scrollTop + el.clientHeight > el.scrollHeight - 10 &&
			action === ReaderAction.Next
		) {
			store.switchChapter(1)
		} else {
			// Scroll up/down by 95% of the screen height
			el.scrollTo({
				top:
					el.scrollTop +
					(action === ReaderAction.Previous ? -1 : 1) * 0.95 * el.clientHeight,
				left: 0,
				behavior: 'smooth'
			})
		}
	}
}
</script>

<style></style>
