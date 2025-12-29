<template>
	<VDialog
		:model-value="modelValue"
		@update:model-value="$emit('update:modelValue', $event)"
		max-width="400"
	>
		<VCard>
			<VCardTitle>Add to list</VCardTitle>
			<VCardText>
				<VAlert v-if="qLists.error.value" type="error" class="mb-4">
					{{ qLists.error.value?.message }}
				</VAlert>
				<VAlert v-if="qInLists.error.value" type="error" class="mb-4">
					{{ qInLists.error.value?.message }}
				</VAlert>

				<div
					v-if="qLists.isLoading.value || qInLists.isLoading.value"
					class="py-10 text-center"
				>
					<VProgressCircular indeterminate />
				</div>

				<VList v-else>
					<VListItem
						v-for="list in qLists.data?.value ?? []"
						:key="list.id"
						:title="list.name"
						:subtitle="list.visibility"
						@click="toggleList(list.id)"
					>
						<template #append>
							<VProgressCircular
								v-if="pendingListIds.has(list.id)"
								indeterminate
								size="24"
								width="2"
							/>
							<VIcon
								v-else-if="inListIds.has(list.id)"
								icon="mdi-check"
								color="success"
							/>
						</template>
					</VListItem>
					<div v-if="!qLists.data?.value?.length" class="text-medium-emphasis">
						No lists yet. Create one from the Lists page.
					</div>
				</VList>

				<VAlert v-if="errorMessage" type="error" class="mt-4">
					{{ errorMessage }}
				</VAlert>
			</VCardText>
			<VCardActions>
				<VSpacer />
				<VBtn variant="text" @click="$emit('update:modelValue', false)">Close</VBtn>
			</VCardActions>
		</VCard>
	</VDialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useQueryClient } from '@tanstack/vue-query'
import { customListsApi } from '@/utils/api/custom-lists'
import { contentApi } from '@/utils/api/content'

const props = defineProps<{
	modelValue: boolean
	contentId: string
}>()

defineEmits<{
	'update:modelValue': [value: boolean]
}>()

const queryClient = useQueryClient()

const qLists = customListsApi.useList('me')
const qInLists = contentApi.useLists(() => props.contentId)

const inListIds = computed(() => new Set(qInLists.data?.value ?? []))

const mCreateEntry = customListsApi.useCreateEntry()
const mDeleteEntry = customListsApi.useDeleteEntry()

const pendingListIds = ref(new Set<string>())
const errorMessage = ref<string | null>(null)

async function toggleList(listId: string) {
	errorMessage.value = null
	pendingListIds.value.add(listId)

	try {
		if (!inListIds.value.has(listId)) {
			await mCreateEntry.mutateAsync({ listId, content_id: props.contentId })
		} else {
			const detail = await customListsApi.get(listId)
			const entry = detail.entries.find(e => e.content_id === props.contentId)
			if (!entry) {
				throw new Error('List entry not found')
			}
			await mDeleteEntry.mutateAsync({ listId, entryId: entry.id })
		}

		queryClient.invalidateQueries({ queryKey: ['content', 'lists', props.contentId] })
		queryClient.invalidateQueries({ queryKey: ['custom-lists'] })
		queryClient.invalidateQueries({ queryKey: ['custom-lists', listId] })
	} catch (err) {
		errorMessage.value = err instanceof Error ? err.message : String(err)
	} finally {
		pendingListIds.value.delete(listId)
	}
}
</script>
