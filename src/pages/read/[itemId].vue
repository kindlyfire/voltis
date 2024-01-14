<template>
	<div v-if="!itemId" class="acontainer">Page not found.</div>
	<ComicReader v-else :item-id="itemId" />
</template>

<script lang="ts" setup>
import ComicReader from '../../components/read/comic/ComicReader.vue'

const route = useRoute()
const itemId = computed(() => {
	return typeof route.params.itemId === 'string' ? route.params.itemId : null
})
</script>

<style></style>
