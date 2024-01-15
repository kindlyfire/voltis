<template>
	<div class="acontainer">
		<CollectionList :collections="qCollections.data.value ?? []" />
	</div>
</template>

<script lang="ts" setup>
import CollectionList from '../components/CollectionList.vue'
import { trpc } from '../plugins/trpc'

const qCollections = trpc.collections.query.useLazyQuery({})
</script>

<style>
img.cover {
	width: 100%;
	aspect-ratio: 158/230;
	object-fit: cover;
}
</style>
