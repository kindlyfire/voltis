<template>
    <ADialog
        :open="open"
        title="Add to lists"
        :description="`${plural(contentIds.length, 'item')} selected`"
        size="sm"
        @update:open="v => !v && close()"
    >
        <div class="flex flex-col gap-4">
            <QueryError :query="qLists" />

            <div v-if="qLists.isLoading.value" class="flex justify-center py-8">
                <ASpinner />
            </div>

            <fieldset v-else-if="qLists.isSuccess.value" class="flex flex-col">
                <legend class="sr-only">Lists</legend>
                <ACheckbox
                    v-for="list in qLists.data.value ?? []"
                    :key="list.id"
                    :model-value="selectedListIds.has(list.id)"
                    :label="list.name"
                    @update:model-value="toggleList(list.id)"
                >
                    {{ list.name }}
                    <span class="text-fg-muted text-sm capitalize">· {{ list.visibility }}</span>
                </ACheckbox>
                <p v-if="!qLists.data.value?.length" class="text-fg-muted">
                    No lists yet. Create one from the Lists page.
                </p>
            </fieldset>

            <QueryError :mutation="mBulk" />
        </div>
        <template #actions>
            <AButton variant="text" tone="neutral" @click="close()">Cancel</AButton>
            <AButton
                :loading="mBulk.isPending.value"
                :disabled="selectedListIds.size === 0"
                @click="save"
            >
                Add
            </AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACheckbox from '@/ui/ACheckbox.vue'
import ADialog from '@/ui/ADialog.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { useToast } from '@/ui/useToast'
import { customListsApi } from '@/utils/api/custom-lists'
import type { CustomListBulkCreateEntry } from '@/utils/api/types'
import { plural } from '@/utils/misc'

const props = defineProps<{
    open: boolean
    close: () => void
    contentIds: string[]
}>()

const qLists = customListsApi.useList('me')
const mBulk = customListsApi.useBulkCreateEntries()
const toast = useToast()

const selectedListIds = ref(new Set<string>())

function toggleList(id: string) {
    const next = new Set(selectedListIds.value)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    selectedListIds.value = next
}

async function save() {
    const entries: CustomListBulkCreateEntry[] = []
    for (const listId of selectedListIds.value) {
        for (const contentId of props.contentIds) {
            entries.push({ list_id: listId, content_id: contentId })
        }
    }
    await mBulk.mutateAsync(entries)
    toast.show({
        message: `Added ${plural(props.contentIds.length, 'item')} to ${plural(selectedListIds.value.size, 'list')}`,
    })
    props.close()
}
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './ListsModal.vue'

export function showBulkListsModal(contentIds: string[]): Promise<void> {
    return Modals.show(Self, { contentIds })
}
</script>
