<template>
	<UButton size="lg" :to="readUrl">
		<UIcon name="ph:book-open-bold" dynamic class="h-4 scale-[1.4]" />
		{{ hasStartedReading ? 'Continue' : 'Start' }} reading
	</UButton>
</template>

<script lang="ts" setup>
import { useQuery } from '@tanstack/vue-query'
import type { useItems } from '../../../../state/composables/queries'
import { trpc } from '../../../../plugins/trpc'

const props = defineProps<{
	collectionId: string
	qItems: ReturnType<typeof useItems>
}>()
const emit = defineEmits<{}>()
const items = props.qItems.data

const qReadStatus = useQuery({
	queryKey: ['read-status', toRef(props, 'collectionId')],
	async queryFn() {
		return await trpc.collections.getReadStatus.query({
			collectionId: props.collectionId
		})
	}
})

const readingItem = computed(() => {
	const readingItemId = qReadStatus.data.value?.reading
	if (readingItemId)
		return items.value?.find(i => i.id === readingItemId) ?? null
	return null
})

const readUrl = computed(() => {
	const item = readingItem.value || items.value?.[0]
	if (!item) return
	return routeBuilder['/read/[itemId]/[page]'](
		item.id,
		qReadStatus.data.value?.progress?.page ?? 0
	)
})

const hasStartedReading = computed(() => {
	const readStatus = qReadStatus.data.value
	if (!readStatus) return false

	if (readStatus.progress?.page) return true

	const index = items.value?.indexOf(readingItem.value!)
	if (index === undefined) return false

	return index !== -1 && index !== items.value!.length - 1
})
</script>

<style></style>
