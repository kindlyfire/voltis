<template>
    <VContainer>
        <h1 class="text-h4 mb-2">Broken References</h1>
        <p class="text-medium-emphasis mb-6 max-w-[700px]">
            Reading status, ratings, and other user data is linked to content by a URI. If content
            is deleted from the server, or the URI otherwise changes in a way we don't handle
            automatically, your data will show up here so that it can be reassigned or deleted.
        </p>

        <div class="d-flex align-center mb-6 gap-2">
            <VSelect
                v-model="selectedLibraryId"
                :items="libraryItems"
                item-title="label"
                item-value="id"
                label="Library"
                density="compact"
                hide-details
                style="max-width: 300px"
            />
            <VTextField
                v-model="searchInput"
                label="Search refs"
                density="compact"
                hide-details
                clearable
                prepend-inner-icon="mdi-magnify"
                style="max-width: 300px"
            />
            <VBtn
                color="primary"
                :disabled="edits.size === 0 || mSave.isPending.value"
                :loading="mSave.isPending.value"
                @click="mSave.mutate()"
            >
                Save
            </VBtn>
            <VMenu>
                <template #activator="{ props: menuProps }">
                    <VBtn v-bind="menuProps" variant="tonal"> Bulk actions </VBtn>
                </template>
                <VList>
                    <VListItem @click="markAllDelete">
                        <VListItemTitle>Mark all to delete</VListItemTitle>
                    </VListItem>
                    <VListItem @click="unmarkAllDeletes">
                        <VListItemTitle>Unmark all deletes</VListItemTitle>
                    </VListItem>
                </VList>
            </VMenu>
        </div>

        <AQueryError :mutation="mSave" class="mb-4" closable />

        <VTable v-if="selectedLibraryId" density="compact">
            <thead>
                <tr>
                    <th>Broken ref</th>
                    <th>Data</th>
                    <th style="width: 50%">Action</th>
                </tr>
            </thead>
            <tbody>
                <tr
                    v-for="item in qBrokenRefs.data.value?.data ?? []"
                    :key="item.id"
                    class="align-middle"
                >
                    <td class="py-2" style="vertical-align: middle">
                        <code>{{ item.uri }}</code>
                    </td>
                    <td class="py-2" style="vertical-align: middle">
                        <div class="d-flex align-center gap-2">
                            <span class="text-medium-emphasis">{{ summarize(item) }}</span>
                            <VBtn
                                icon
                                size="x-small"
                                variant="text"
                                class="ml-1"
                                @click="showBrokenRefDetailModal(item)"
                            >
                                <VIcon size="small">mdi-eye</VIcon>
                            </VBtn>
                        </div>
                    </td>
                    <td class="py-2" style="vertical-align: middle">
                        <div class="d-flex align-center gap-2">
                            <VTooltip v-if="shouldShowUriWarning(item.id)" location="top">
                                <template #activator="{ props: tp }">
                                    <VIcon v-bind="tp" color="warning" size="small">
                                        mdi-alert
                                    </VIcon>
                                </template>
                                <div class="max-w-[250px]">
                                    This ref already has user data. If you save, the current entry
                                    will be replaced by this older entry.
                                </div>
                            </VTooltip>
                            <VAutocomplete
                                :model-value="
                                    getEdit(item.id) !== 'delete'
                                        ? (getEdit(item.id) ?? null)
                                        : null
                                "
                                @update:model-value="v => setEdit(item.id, v || undefined)"
                                :items="qUris.data.value?.content_uris ?? []"
                                :disabled="getEdit(item.id) === 'delete'"
                                label="New ref"
                                density="compact"
                                hide-details
                                clearable
                                class="grow"
                            />
                            <VBtn
                                icon
                                size="small"
                                variant="text"
                                :color="getEdit(item.id) === 'delete' ? 'error' : undefined"
                                @click="toggleDelete(item.id)"
                            >
                                <VIcon>mdi-delete</VIcon>
                            </VBtn>
                        </div>
                    </td>
                </tr>
                <tr v-if="qBrokenRefs.data.value?.data.length === 0">
                    <td colspan="3" class="text-medium-emphasis pa-4 text-center">
                        No broken references found.
                    </td>
                </tr>
            </tbody>
        </VTable>

        <div
            v-if="selectedLibraryId && (qBrokenRefs.data.value?.total ?? 0) > PAGE_SIZE"
            class="d-flex mt-4 justify-center"
        >
            <VPagination
                :model-value="page"
                @update:model-value="page = $event"
                :length="Math.ceil((qBrokenRefs.data.value?.total ?? 0) / PAGE_SIZE)"
            />
        </div>
    </VContainer>
</template>

<script setup lang="ts">
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { useDebounceFn } from '@vueuse/core'
import { ref, computed, watch, reactive } from 'vue'
import AQueryError from '@/components/AQueryError.vue'
import { contentApi } from '@/utils/api/content'
import { librariesApi } from '@/utils/api/libraries'
import { READING_STATUS_LABELS } from '@/utils/api/types'
import type { BrokenUserToContent } from '@/utils/api/types'
import { showBrokenRefDetailModal } from './BrokenRefDetailModal.vue'

const PAGE_SIZE = 50

const queryClient = useQueryClient()
const qSummary = contentApi.useBrokenRefsSummary()
const qLibraries = librariesApi.useList()

const selectedLibraryId = ref<string | null>(null)
const searchInput = ref('')
const search = ref('')
const page = ref(1)
const edits = reactive(new Map<string, string | 'delete'>())

const debouncedSearch = useDebounceFn((val: string) => {
    search.value = val
    page.value = 1
}, 300)
watch(searchInput, val => debouncedSearch(val ?? ''))

const libraryItems = computed(() => {
    const summaryData = qSummary.data.value ?? []
    const libraries = qLibraries.data.value ?? []
    return summaryData.map(s => {
        const lib = libraries.find(l => l.id === s.library_id)
        return {
            id: s.library_id,
            label: `${lib?.name ?? s.library_id} (${s.count})`,
        }
    })
})

watch(
    libraryItems,
    items => {
        const first = items?.[0]
        if (!items?.some(i => i.id === selectedLibraryId.value)) {
            selectedLibraryId.value = null
        }
        if (!selectedLibraryId.value && first?.id) {
            selectedLibraryId.value = first.id
        }
    },
    { immediate: true }
)

const qBrokenRefs = contentApi.useBrokenRefs(selectedLibraryId, () => ({
    search: search.value || undefined,
    limit: PAGE_SIZE,
    offset: (page.value - 1) * PAGE_SIZE,
}))
const qUris = contentApi.useLibraryUris(selectedLibraryId)
const userUriSet = computed(() => new Set(qUris.data.value?.user_uris ?? []))

watch([selectedLibraryId, search], () => {
    edits.clear()
    page.value = 1
})

function getEdit(id: string): string | 'delete' | undefined {
    return edits.get(id)
}

function setEdit(id: string, uri: string | undefined) {
    if (uri) {
        edits.set(id, uri)
    } else {
        edits.delete(id)
    }
}

function markAllDelete() {
    for (const item of qBrokenRefs.data.value?.data ?? []) {
        edits.set(item.id, 'delete')
    }
}

function unmarkAllDeletes() {
    for (const [id, value] of [...edits]) {
        if (value === 'delete') edits.delete(id)
    }
}

function toggleDelete(id: string) {
    if (edits.get(id) === 'delete') {
        edits.delete(id)
    } else {
        edits.set(id, 'delete')
    }
}

function shouldShowUriWarning(id: string) {
    const edit = edits.get(id)
    return edit && edit !== 'delete' && userUriSet.value.has(edit)
}

const mSave = useMutation({
    mutationFn: async () => {
        if (!selectedLibraryId.value) return
        const toDelete: string[] = []
        const toUpdate: Record<string, string> = {}
        for (const [id, value] of edits) {
            if (value === 'delete') {
                toDelete.push(id)
            } else {
                toUpdate[id] = value
            }
        }
        await contentApi.fixBrokenRefs(selectedLibraryId.value, {
            delete: toDelete,
            update: toUpdate,
        })
    },
    onSuccess: () => {
        edits.clear()
        queryClient.invalidateQueries({ queryKey: ['content', 'broken-refs'] })
        queryClient.invalidateQueries({ queryKey: ['content', 'broken-refs-summary'] })
    },
})

function summarize(item: BrokenUserToContent): string {
    const parts: string[] = []
    if (item.status) parts.push(READING_STATUS_LABELS[item.status])
    if (item.rating != null) parts.push(`rating: ${item.rating}`)
    if (item.notes) parts.push('has notes')
    return parts.join(', ') || '—'
}
</script>
