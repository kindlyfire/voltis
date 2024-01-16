<template>
	<div class="acontainer" v-if="store.item && store.collection">
		<div class="text-lg font-semibold">
			{{ store.item.name }}
		</div>
		<div>
			<NuxtLink
				:to="'/' + slugify(store.collection.name) + ':' + store.collection.id"
				class="text-primary hover:underline"
			>
				{{ store.collection.name }}
			</NuxtLink>
		</div>
	</div>
	<ComicReaderMain ref="mainRef" />
	<USlideover
		side="right"
		v-model="store.menuOpen"
		:ui="{
			width: 'max-w-[300px]'
		}"
		:transition="false"
	>
		<ComicReaderMenu />
	</USlideover>
</template>

<script lang="ts" setup>
import slugify from 'slugify'
import ComicReaderMain from './ComicReaderMain.vue'
import { useComicReaderStore } from '../state'
import ComicReaderMenu from './ComicReaderMenu.vue'

const props = defineProps<{
	itemId: string
}>()
const emit = defineEmits<{}>()

const store = useComicReaderStore()
watchEffect(() => {
	store.itemId = props.itemId
})

const mainRef = ref<InstanceType<typeof ComicReaderMain> | null>(null)
const scrolledForItem = ref('')
watchEffect(() => {
	if (
		store.item &&
		store.collection &&
		mainRef.value?.readerMainRef &&
		store.readerPages.length > 0 &&
		store.readerState.pageIndex === 0 &&
		scrolledForItem.value !== store.item.id
	) {
		scrolledForItem.value = store.item.id
		setTimeout(() => {
			document.getElementById('mainOverflowArea')?.scrollTo({
				top: mainRef.value!.readerMainRef!.offsetTop,
				left: 0,
				behavior: 'smooth'
			})
		}, 150)
	}
})

defineShortcuts({
	ArrowLeft: () => {
		if (!store.menuOpen) store.switchPage(-1)
	},
	ArrowRight: () => {
		if (!store.menuOpen) store.switchPage(1)
	}
})
</script>

<style></style>
