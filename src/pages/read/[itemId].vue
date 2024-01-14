<template>
	<div class="acontainer">
		<div>Reading item</div>
		<!-- <div>
			{{ qItem.data }}
		</div> -->
		<!-- <div>
			{{ qReaderData.data }}
		</div> -->
		<div v-if="qReaderData.data.value?.files.length">
			<img v-for="url in filesUrls" :src="url" />
		</div>
	</div>
</template>

<script lang="ts" setup>
import { useQuery } from '@tanstack/vue-query'
import { trpc } from '../../plugins/trpc'

const route = useRoute()
const qItem = useQuery({
	queryKey: ['item', route.params.itemId],
	async queryFn() {
		return trpc.items.get.query({ id: route.params.itemId as string })
	},
	enabled: computed(() => typeof route.params.itemId === 'string')
})

const qReaderData = useQuery({
	queryKey: ['reader-data', route.params.itemId],
	async queryFn() {
		return trpc.items.getReaderData.query({ id: route.params.itemId as string })
	},
	enabled: computed(() => typeof route.params.itemId === 'string')
})

const filesUrls = computed(() => {
	if (!qReaderData.data.value) return []
	return qReaderData.data.value.files.map(f => {
		return (
			'/api/comic-page?item-id=' +
			encodeURIComponent(route.params.itemId as string) +
			'&file-name=' +
			encodeURIComponent(f.name)
		)
	})
})
</script>

<style></style>
