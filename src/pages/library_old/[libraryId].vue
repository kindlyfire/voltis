<template>
	<div class="acontainer flex flex-col gap-4">
		<PageTitle :title="library.name" />
		<div>
			<CollectionList :collections="qCollections.data.value ?? []" />
		</div>
	</div>
</template>

<script lang="ts" setup>
import { useQuery } from '@tanstack/vue-query'
import { useLibraries } from '../../state/composables/queries'
import { trpc } from '../../plugins/trpc'

const route = useRoute()
const qLibraries = useLibraries({})
await qLibraries.suspense()
const library = computed(
	() => qLibraries.data.value?.find(l => l.id === route.params.libraryId)!
)
if (!library.value) {
	throw createError({
		statusCode: 404,
		message: 'Library not found'
	})
}

const qCollections = useQuery({
	queryKey: [
		'collections',
		computed(() => JSON.stringify({ libraryId: route.params.libraryId }))
	],
	async queryFn() {
		return trpc.collections.query.query({
			libraryIds: [library.value!.id!]
		})
	},
	enabled: computed(() => !!library.value)
})
</script>

<style></style>
