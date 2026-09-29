<template>
    <ADialog :open="open" title="Add to list" size="sm" @update:open="v => !v && close()">
        <div class="flex flex-col gap-4">
            <QueryError :query="qLists" />
            <QueryError :query="qInLists" />

            <div
                v-if="qLists.isLoading.value || qInLists.isLoading.value"
                class="flex justify-center py-8"
            >
                <ASpinner />
            </div>

            <template v-else-if="ready">
                <fieldset class="flex flex-col">
                    <legend class="sr-only">Lists</legend>
                    <ACheckbox
                        v-for="list in qLists.data.value ?? []"
                        :key="list.id"
                        :model-value="pendingListIds.has(list.id) !== inListIds.has(list.id)"
                        :label="list.name"
                        :readonly="pendingListIds.has(list.id)"
                        :aria-busy="pendingListIds.has(list.id) || undefined"
                        @update:model-value="toggleList(list)"
                    >
                        {{ list.name }}
                        <span class="text-fg-muted text-sm capitalize"
                            >· {{ list.visibility }}</span
                        >
                    </ACheckbox>
                </fieldset>
                <NewListForm
                    v-if="newList.creating.value"
                    @created="toggleList"
                    @close="newList.stop"
                />
            </template>

            <QueryError :mutation="mToggle" />
        </div>
        <template #actions>
            <AButton
                v-if="ready && !newList.creating.value"
                :ref="newList.buttonRef"
                class="mr-auto"
                variant="text"
                :leading-icon="IconPlus"
                @click="newList.start"
            >
                New list
            </AButton>
            <AButton variant="text" tone="neutral" @click="close()">Close</AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { computed, ref } from 'vue'
import NewListForm, { useNewListToggle } from '@/components/NewListForm.vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACheckbox from '@/ui/ACheckbox.vue'
import ADialog from '@/ui/ADialog.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { IconPlus } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'
import { customListsApi } from '@/utils/api/custom-lists'
import type { CustomListPartial } from '@/utils/api/types'

const props = defineProps<{
    open: boolean
    close: () => void
    contentId: string
}>()

const newList = useNewListToggle()

const queryClient = useQueryClient()
const toast = useToast()

const qLists = customListsApi.useList('me')
const qInLists = contentApi.useLists(() => props.contentId)
const ready = computed(() => qLists.isSuccess.value && qInLists.isSuccess.value)

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

async function toggleList(list: CustomListPartial) {
    const adding = !inListIds.value.has(list.id)
    pendingListIds.value.add(list.id)
    try {
        await mToggle.mutateAsync(list.id)
        toast.show({ message: adding ? `Added to ${list.name}` : `Removed from ${list.name}` })
    } catch {
        // Shown by QueryError.
    } finally {
        pendingListIds.value.delete(list.id)
    }
}
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './ListsModal.vue'

export function showListsModal(contentId: string): Promise<void> {
    return Modals.show(Self, { contentId })
}
</script>
