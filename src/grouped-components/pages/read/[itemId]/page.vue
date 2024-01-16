<template>
	<div v-if="!itemId" class="acontainer">Page not found.</div>
	<ComicReader v-else :item-id="itemId" />
</template>

<script lang="ts" setup>
import ComicReader from './_components/ComicReader.vue'
import { useComicReaderStore } from './state'

const route = useRoute()
const itemId = computed(() => {
	return typeof route.params.itemId === 'string' ? route.params.itemId : null
})
const store = useComicReaderStore()

useSeoMeta({
	title() {
		if (store.item && store.collection) {
			return `${store.item.name} - ${store.collection.name}`
		}
		return 'Loading...'
	}
})
</script>

<style></style>
