<template>
    <VContainer class="py-4">
        <AQueryError :query="qList" class="mb-4" />
        <AQueryError :mutation="mReorderEntries" class="mb-4" />
        <AQueryError :mutation="mDeleteEntry" class="mb-4" />

        <div v-if="!list" class="flex items-center justify-center py-16">
            <VProgressCircular indeterminate size="64" />
        </div>

        <template v-else>
            <div class="flex flex-col gap-6 xl:flex-row xl:items-start">
                <div class="w-full xl:w-1/3">
                    <VCard variant="tonal">
                        <VCardText class="space-y-4! p-5">
                            <h1 class="my-0 text-3xl">{{ list.name }}</h1>
                            <div class="mt-3 flex flex-wrap gap-2">
                                <VChip size="small" variant="flat" class="capitalize">
                                    {{ list.visibility }}
                                </VChip>
                                <VChip size="small" variant="text">
                                    {{ list.entry_count ?? 0 }} entries
                                </VChip>
                            </div>

                            <div
                                v-if="list.description"
                                class="text-sm whitespace-pre-wrap opacity-60"
                            >
                                {{ list.description }}
                            </div>

                            <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
                                <dt class="opacity-60">Created</dt>
                                <dd class="m-0">{{ formatDate(list.created_at) }}</dd>
                                <dt class="opacity-60">Updated</dt>
                                <dd class="m-0">{{ formatDate(list.updated_at) }}</dd>
                            </dl>

                            <VBtn
                                prepend-icon="mdi-pencil"
                                variant="tonal"
                                @click="showListModal(list.id)"
                            >
                                Edit
                            </VBtn>
                        </VCardText>
                    </VCard>
                </div>

                <div class="w-full xl:w-2/3">
                    <h2 class="text-1xl">Entries</h2>

                    <div v-if="entries.length" class="space-y-4!">
                        <template v-for="(entry, idx) in entries" :key="entry.id">
                            <VCard v-if="entry.content">
                                <div class="flex flex-col sm:flex-row">
                                    <div>
                                        <div
                                            class="bg-surface-variant/40 relative aspect-[2.1/3] w-full sm:w-[100px]"
                                        >
                                            <img
                                                v-if="entryCoverUri(entry)"
                                                :src="entryCoverUri(entry)!"
                                                class="absolute h-full w-full object-cover"
                                            />
                                            <div
                                                v-else
                                                class="absolute flex h-full w-full items-center justify-center opacity-60"
                                            >
                                                <div class="text-center">
                                                    <VIcon icon="mdi-image-off-outline" size="36" />
                                                    <div class="mt-2 text-xs">No cover</div>
                                                </div>
                                            </div>
                                        </div>
                                    </div>

                                    <VCardText class="min-w-0 grow p-2 px-4">
                                        <div
                                            class="flex h-full flex-wrap items-start gap-4 sm:flex-nowrap"
                                        >
                                            <div class="min-w-0 grow">
                                                <div class="mb-2 flex flex-wrap items-center gap-2">
                                                    <RouterLink
                                                        :to="`/${entry.content.id}`"
                                                        class="text-h6 min-w-0 font-medium"
                                                    >
                                                        {{ entryTitle(entry) }}
                                                    </RouterLink>
                                                </div>

                                                <div class="mb-4 flex flex-wrap gap-2">
                                                    <VChip size="small" variant="tonal">
                                                        #{{ displayOrder(entry, idx) }}
                                                    </VChip>
                                                    <VChip size="small" variant="tonal">
                                                        {{ displayContentType(entry.content.type) }}
                                                    </VChip>
                                                    <VChip
                                                        size="small"
                                                        variant="tonal"
                                                        :to="`/${entry.library_id}`"
                                                    >
                                                        {{ libraryName(entry.library_id) }}
                                                    </VChip>
                                                </div>

                                                <div class="mt-auto" v-if="entry.notes">
                                                    <div class="mb-1 text-xs opacity-60">Notes</div>
                                                    <div class="text-sm whitespace-pre-wrap">
                                                        {{ entry.notes }}
                                                    </div>
                                                </div>
                                            </div>

                                            <div class="flex flex-row gap-1">
                                                <VBtn
                                                    icon="mdi-pencil"
                                                    variant="text"
                                                    size="x-small"
                                                    @click="
                                                        showEntryModal({
                                                            listId: list.id,
                                                            entryId: entry.id,
                                                            title: entryTitle(entry),
                                                            notes: entry.notes,
                                                        })
                                                    "
                                                    title="Edit notes"
                                                />
                                                <VBtn
                                                    icon="mdi-chevron-up"
                                                    variant="text"
                                                    size="x-small"
                                                    :disabled="
                                                        idx === 0 || mReorderEntries.isPending.value
                                                    "
                                                    @click="moveEntry(entry.id, -1)"
                                                    title="Move up"
                                                />
                                                <VBtn
                                                    icon="mdi-chevron-down"
                                                    variant="text"
                                                    size="x-small"
                                                    :disabled="
                                                        idx === entries.length - 1 ||
                                                        mReorderEntries.isPending.value
                                                    "
                                                    @click="moveEntry(entry.id, 1)"
                                                    title="Move down"
                                                />
                                                <VBtn
                                                    icon="mdi-delete"
                                                    color="error"
                                                    variant="text"
                                                    size="x-small"
                                                    :loading="mDeleteEntry.isPending.value"
                                                    @click="handleDelete(entry.id)"
                                                    title="Delete entry"
                                                />
                                            </div>
                                        </div>
                                    </VCardText>
                                </div>
                            </VCard>
                        </template>
                    </div>
                    <VCard v-else variant="tonal">
                        <VCardText class="opacity-60">No entries yet.</VCardText>
                    </VCard>
                </div>
            </div>
        </template>
    </VContainer>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed } from 'vue'
import { useRoute, RouterLink } from 'vue-router'
import AQueryError from '@/components/AQueryError.vue'
import { customListsApi } from '@/utils/api/custom-lists'
import { librariesApi } from '@/utils/api/libraries'
import type { CustomListEntry } from '@/utils/api/types'
import { API_URL } from '@/utils/fetch'
import { displayContentType } from '@/utils/misc'
import { showEntryModal } from './EntryModal.vue'
import { showListModal } from './ListModal.vue'

const route = useRoute()
const listId = computed(() => route.params.id as string)

const qList = customListsApi.useGet(listId)
const list = computed(() => qList.data.value)
const entries = computed(() => list.value?.entries ?? [])

const qLibraries = librariesApi.useList()
const mDeleteEntry = customListsApi.useDeleteEntry()
const mReorderEntries = customListsApi.useReorderEntries()

useHead({
    title() {
        return list.value?.name ?? 'List'
    },
})

function libraryName(id: string) {
    return qLibraries.data.value?.find(l => l.id === id)?.name ?? id
}

function formatDate(value: string) {
    return new Date(value).toLocaleDateString()
}

function displayOrder(entry: CustomListEntry, idx: number) {
    return entry.order ?? idx
}

function entryTitle(entry: CustomListEntry) {
    return entry.content?.title || entry.uri
}

function entryCoverUri(entry: CustomListEntry) {
    if (!entry.content?.cover_uri) return null
    return `${API_URL}/files/cover/${entry.content.id}?v=${entry.content.file_mtime}`
}

async function handleDelete(entryId: string) {
    await mDeleteEntry.mutateAsync({ listId: listId.value, entryId })
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
    await mReorderEntries.mutateAsync({ listId: listId.value, ctc_ids: newOrder })
}
</script>
