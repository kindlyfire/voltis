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
	<ComicReaderMain ref="mainRef" />
</template>

<script lang="ts" setup>
import slugify from 'slugify'
import { readerStateKey, useComicReaderStore } from '../state'
import ComicReaderMain from './ComicReaderMain.vue'

const props = defineProps<{
	itemId: string
}>()
const emit = defineEmits<{}>()

const store = useComicReaderStore()
provide(readerStateKey, store)
watchEffect(() => {
	store.itemId.value = props.itemId
})

const mainRef = ref<InstanceType<typeof ComicReaderMain> | null>(null)
const scrolledForItem = ref('')
watchEffect(() => {
	if (
		store.item.value &&
		store.collection.value &&
		mainRef.value?.readerMainRef &&
		store.readerPages.value.length > 0 &&
		store.readerState.pageIndex === 0 &&
		scrolledForItem.value !== store.item.value.id
	) {
		scrolledForItem.value = store.item.value.id
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
		store.switchPage(-1)
	},
	ArrowRight: () => {
		store.switchPage(1)
	}
})
</script>

<style></style>
