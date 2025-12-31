<template>
    <VContainer>
        <AQueryError :query="qList" class="mb-4" />

        <div v-if="!list" class="flex items-center justify-center py-16">
            <VProgressCircular indeterminate size="64" />
        </div>

        <template v-else>
            <div class="d-flex align-center mb-4">
                <div>
                    <h1 class="text-h4 mb-1">{{ list.name }}</h1>
                    <div class="text-caption text-medium-emphasis">
                        Visibility: {{ list.visibility }} • Entries: {{ list.entry_count ?? 0 }}
                    </div>
                </div>
                <VSpacer />
                <RouterLink :to="{ name: 'lists' }">
                    <VBtn variant="text" prepend-icon="mdi-arrow-left">Back</VBtn>
                </RouterLink>
            </div>

            <VTable v-if="entries.length">
                <thead>
                    <tr>
                        <th style="width: 70px">Order</th>
                        <th>Content</th>
                        <th>Notes</th>
                        <th style="width: 160px">Actions</th>
                    </tr>
                </thead>
                <tbody>
                    <tr v-for="(entry, idx) in entries" :key="entry.id">
                        <td>#{{ entry.order ?? idx }}</td>
                        <td>
                            <div class="flex items-center gap-2">
                                <RouterLink
                                    v-if="entry.content?.id"
                                    :to="`/${entry.content.id}`"
                                    class="text-primary"
                                >
                                    {{ entry.uri }}
                                </RouterLink>
                                <span v-else>{{ entry.uri }}</span>
                                <CopyIdButton v-if="entry.content?.id" :id="entry.content.id" />
                            </div>
                            <div class="text-caption text-medium-emphasis">
                                {{ entry.library_id }}
                            </div>
                        </td>
                        <td>
                            <!-- <VTextarea
								v-model="noteDrafts[entry.id]"
								variant="underlined"
								auto-grow
								rows="1"
								hide-details
							/> -->
                        </td>
                        <td class="flex items-center gap-2">
                            <VBtn
                                icon="mdi-chevron-up"
                                variant="text"
                                size="small"
                                :disabled="idx === 0 || reorder.isPending.value"
                                @click="moveEntry(entry.id, -1)"
                                title="Move up"
                            />
                            <VBtn
                                icon="mdi-chevron-down"
                                variant="text"
                                size="small"
                                :disabled="idx === entries.length - 1 || reorder.isPending.value"
                                @click="moveEntry(entry.id, 1)"
                                title="Move down"
                            />
                            <!-- <VBtn
								icon="mdi-content-save"
								variant="text"
								size="small"
								:loading="updateEntry.isPending.value"
								@click="saveNotes(entry.id)"
								title="Save notes"
							/> -->
                            <VBtn
                                icon="mdi-delete"
                                color="error"
                                variant="text"
                                size="small"
                                :loading="deleteEntry.isPending.value"
                                @click="handleDelete(entry.id)"
                                title="Delete entry"
                            />
                        </td>
                    </tr>
                </tbody>
            </VTable>
            <div v-else class="text-medium-emphasis">No entries yet.</div>
        </template>
    </VContainer>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, RouterLink } from 'vue-router'
import { useHead } from '@unhead/vue'
import { customListsApi } from '@/utils/api/custom-lists'
import CopyIdButton from '@/pages/settings/CopyIdButton.vue'
import AQueryError from '@/components/AQueryError.vue'

const route = useRoute()
const listId = computed(() => route.params.id as string)

const qList = customListsApi.useGet(listId)
const list = computed(() => qList.data.value)
const entries = computed(() => list.value?.entries ?? [])

const deleteEntry = customListsApi.useDeleteEntry()
const reorder = customListsApi.useReorderEntries()

useHead({
    title() {
        return list.value?.name ?? 'List'
    },
})

async function handleDelete(entryId: string) {
    await deleteEntry.mutateAsync({ listId: listId.value, entryId })
}

async function moveEntry(entryId: string, delta: number) {
    if (!entries.value?.length) return
    const orderIds = entries.value.map(e => e.id)
    const idx = orderIds.indexOf(entryId)
    if (idx < 0) return
    const newIdx = idx + delta
    if (newIdx < 0 || newIdx >= orderIds.length) return
    const newOrder = [...orderIds]
    const [moved] = newOrder.splice(idx, 1)
    newOrder.splice(newIdx, 0, moved!)
    await reorder.mutateAsync({ listId: listId.value, ctc_ids: newOrder })
}
</script>
