<template>
    <VDialog
        :model-value="modelValue"
        @update:model-value="$emit('update:modelValue', $event)"
        max-width="400"
    >
        <VCard>
            <VCardTitle>Add to list</VCardTitle>
            <VCardText class="space-y-4!">
                <AQueryError :query="qLists" />
                <AQueryError :query="qInLists" />

                <div
                    v-if="qLists.isLoading.value || qInLists.isLoading.value"
                    class="py-10 text-center"
                >
                    <VProgressCircular indeterminate />
                </div>

                <VList v-else-if="qLists.isSuccess.value && qInLists.isSuccess.value">
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

                <AQueryError :mutation="mToggle" />
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
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { customListsApi } from '@/utils/api/custom-lists'
import { contentApi } from '@/utils/api/content'
import AQueryError from '@/components/AQueryError.vue'

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

const mToggle = useMutation({
    mutationFn: async (listId: string) => {
        if (!inListIds.value.has(listId)) {
            await mCreateEntry.mutateAsync({ listId, content_id: props.contentId })
        } else {
            const detail = await customListsApi.get(listId)
            const entry = detail.entries.find(e => e.content?.id === props.contentId)
            if (!entry) {
                throw new Error('List entry not found')
            }
            await mDeleteEntry.mutateAsync({ listId, entryId: entry.id })
        }

        await queryClient.invalidateQueries({ queryKey: ['content', 'lists', props.contentId] })
        await queryClient.invalidateQueries({ queryKey: ['custom-lists'] })
        await queryClient.invalidateQueries({ queryKey: ['custom-lists', listId] })
    },
})

async function toggleList(listId: string) {
    pendingListIds.value.add(listId)
    try {
        await mToggle.mutateAsync(listId)
    } finally {
        pendingListIds.value.delete(listId)
    }
}
</script>
