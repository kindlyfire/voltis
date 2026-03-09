<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close()" max-width="400">
        <VCard>
            <VCardTitle>Add to lists</VCardTitle>
            <VCardText class="space-y-4!">
                <AQueryError :query="qLists" />

                <div v-if="qLists.isLoading.value" class="py-10 text-center">
                    <VProgressCircular indeterminate />
                </div>

                <VList v-else-if="qLists.isSuccess.value">
                    <VListItem
                        v-for="list in qLists.data?.value ?? []"
                        :key="list.id"
                        :title="list.name"
                        :subtitle="list.visibility"
                        @click="toggleList(list.id)"
                    >
                        <template #append>
                            <VIcon
                                v-if="selectedListIds.has(list.id)"
                                icon="mdi-check"
                                color="success"
                            />
                        </template>
                    </VListItem>
                    <div v-if="!qLists.data?.value?.length" class="text-medium-emphasis">
                        No lists yet. Create one from the Lists page.
                    </div>
                </VList>

                <AQueryError :mutation="mBulk" />
            </VCardText>
            <VCardActions>
                <VSpacer />
                <VBtn variant="text" @click="close()">Cancel</VBtn>
                <VBtn
                    color="primary"
                    :loading="mBulk.isPending.value"
                    :disabled="selectedListIds.size === 0"
                    @click="save"
                >
                    Save
                </VBtn>
            </VCardActions>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import AQueryError from '@/components/AQueryError.vue'
import { customListsApi } from '@/utils/api/custom-lists'
import type { CustomListBulkCreateEntry } from '@/utils/api/types'

const props = defineProps<{
    open: boolean
    close: () => void
    contentIds: string[]
}>()

const qLists = customListsApi.useList('me')
const mBulk = customListsApi.useBulkCreateEntries()

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
