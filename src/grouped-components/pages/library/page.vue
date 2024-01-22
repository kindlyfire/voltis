<template>
	<div class="acontainer flex flex-col gap-4">
		<PageTitle title="Library" />
		<div class="flex items-start">
			<UTabs :items="tabs"></UTabs>
		</div>
	</div>
</template>

<script lang="ts" setup>
import { useQuery } from '@tanstack/vue-query'
import { trpc } from '../../../plugins/trpc'

const route = useRoute()

const tab = computed(() => {
	const v = route.query.tab
	const tabs = ['reading', 'plan to read', 'on hold', 're-reading', 'dropped']
	return tabs.includes(v as any) ? (v as string) : 'reading'
})

const qItems = useQuery({
	queryKey: ['library', tab],
	async queryFn() {
		const items = await trpc.customLists.getUserLibraryItems.query({
			type: tab.value as any
		})
		return items
	}
})
const items = qItems.data

const tabs = [
	{
		label: 'Reading',
		type: 'reading'
	},
	{
		label: 'Plan to read',
		type: 'plan to read'
	},
	{
		label: 'On hold',
		type: 'on hold'
	},
	{
		label: 'Re-reading',
		type: 're-reading'
	},
	{
		label: 'Dropped',
		type: 'dropped'
	}
]
</script>

<style></style>
