<template>
	<USelectMenu
		:options="optionsWithRemove"
		v-model="selectedOption"
		value-attribute="type"
		label-attribute="label"
		size="lg"
		:disabled="mSaveToLibrary.isPending.value"
	>
		<template #label>
			<span v-if="selectedOption" class="truncate">{{
				options.find(o => o.type === selectedOption)!.label
			}}</span>
			<span v-else>Add to library</span>
		</template>
	</USelectMenu>
</template>

<script lang="ts" setup>
import { useMutation, useQuery } from '@tanstack/vue-query'
import { trpc } from '../../../../plugins/trpc'

const props = defineProps<{
	collectionId: string
}>()
const emit = defineEmits<{}>()

const qListsForItem = useQuery({
	queryKey: ['lists-for-item', toRef(props, 'collectionId')],
	async queryFn() {
		const lists = await trpc.customLists.getUserListsForCollection.query({
			id: props.collectionId,
			types: ['reading', 'plan to read', 'on hold', 're-reading', 'dropped']
		})
		return lists
	}
})

const mSaveToLibrary = useMutation({
	async mutationFn(type: string) {
		if (type === 'remove')
			await trpc.customLists.deleteCollection.mutate({
				id: props.collectionId
			})
		else
			await trpc.customLists.addCollectionToLibrary.mutate({
				id: props.collectionId,
				type: type as any
			})
		await qListsForItem.refetch()
	}
})

const selectedOption = computed({
	get() {
		return qListsForItem.data.value?.find(list => list.type !== 'custom')?.type
	},
	set(v) {
		if (!v) return
		mSaveToLibrary.mutate(v)
	}
})

const options = [
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

const optionsWithRemove = computed(() => {
	if (selectedOption.value) {
		return [
			...options,
			{
				label: '(remove)',
				type: 'remove'
			}
		]
	}
	return options
})
</script>

<style></style>
