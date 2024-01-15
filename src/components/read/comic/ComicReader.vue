<template>
	<div class="acontainer" v-if="store.item.value && store.collection.value">
		<div class="text-lg font-semibold">
			{{ store.item.value.name }}
		</div>
		<div>
			<NuxtLink
				:to="
					'/' +
					slugify(store.collection.value.name) +
					':' +
					store.collection.value.id
				"
				class="text-primary hover:underline"
			>
				{{ store.collection.value.name }}
			</NuxtLink>
		</div>
		<div>
			<UButton @click="store.switchMode()">Switch mode</UButton>
		</div>
	</div>
	<div
		class="-mb-4 mt-4"
		:class="modeClasses[store.readerMode.value].main"
		ref="readerMainRef"
		@click="onReaderClick"
	>
		<template v-for="p in store.readerPages.value">
			<div
				v-if="p.error || !p.blobUrl"
				:style="
					store.readerMode.value === 'longstrip' && {
						width: p.file.width + 'px',
						height: p.file.height + 'px'
					}
				"
			>
				{{ p.error || 'Loading...' }}
			</div>
			<img
				v-else
				:src="p.blobUrl"
				alt=""
				:class="modeClasses[store.readerMode.value].images"
			/>
		</template>
	</div>
</template>

<script lang="ts" setup>
import slugify from 'slugify'
import { useComicReaderStore } from './state'
import { useScroll, useWindowScroll, watchDebounced } from '@vueuse/core'

const props = defineProps<{
	itemId: string
}>()
const emit = defineEmits<{}>()

const readerMainRef = ref<HTMLDivElement | null>(null)

const modeClasses = {
	pages: {
		main: 'h-screen w-screen shrink-0 flex flex-row items-center justify-center cursor-pointer',
		images: 'max-h-full max-w-full'
	},
	longstrip: {
		main: 'w-screen flex flex-col items-center justify-center cursor-pointer',
		images: 'max-w-full'
	}
}

const store = useComicReaderStore()
watchEffect(() => {
	store.itemId.value = props.itemId
})

const scrolledForItem = ref('')
watchEffect(() => {
	if (
		store.item.value &&
		store.collection.value &&
		readerMainRef.value &&
		store.readerPages.value.length > 0 &&
		store.readerState.pageIndex === 0 &&
		scrolledForItem.value !== store.item.value.id
	) {
		scrolledForItem.value = store.item.value.id
		setTimeout(() => {
			document.getElementById('mainOverflowArea')?.scrollTo({
				top: readerMainRef.value!.offsetTop,
				left: 0,
				behavior: 'smooth'
			})
		}, 150)
	}
})

defineShortcuts({
	ArrowLeft: () => {
		store.switchPage(-1)
	},
	ArrowRight: () => {
		store.switchPage(1)
	}
})

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

const mainOverflowArea = ref<HTMLElement | null>(null)
onMounted(() => {
	mainOverflowArea.value = document.getElementById('mainOverflowArea')
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
</script>

<style></style>
