<template>
    <div class="settings-page max-w-[1200px]">
        <APageHeader title="Broken references" />
        <p class="text-fg-muted max-w-[700px] text-sm leading-normal">
            Reading status, ratings, and other user data is linked to content by a URI. If content
            is deleted from the server, or the URI otherwise changes in a way we don't handle
            automatically, your data will show up here so that it can be reassigned or deleted.
        </p>

        <QueryError :query="qSummary" />

        <div v-if="qSummary.isLoading.value" class="flex justify-center py-16">
            <ASpinner size="lg" />
        </div>

        <div
            v-else-if="qSummary.isSuccess.value && !libraryOptions.length"
            class="text-fg-muted flex flex-col items-center gap-2 py-16 text-center"
        >
            <h2
                ref="emptyHeading"
                tabindex="-1"
                class="a-focus text-fg font-display text-xl font-semibold"
            >
                No broken references
            </h2>
            <p class="text-sm">All user data points at existing content.</p>
        </div>

        <template v-else-if="libraryOptions.length">
            <div class="flex flex-wrap items-center gap-3">
                <ASelect
                    v-model="selectedLibraryId"
                    :options="libraryOptions"
                    label="Library"
                    class="w-full sm:w-64"
                />
                <ATextField
                    v-model="searchInput"
                    label="Search refs"
                    type="search"
                    :leading-icon="IconMagnify"
                    clearable
                    class="w-full sm:w-64"
                />
                <div class="flex gap-2 sm:ml-auto">
                    <AMenu align="end">
                        <template #trigger>
                            <AButton variant="tonal" :trailing-icon="IconChevronDown">
                                Bulk actions
                            </AButton>
                        </template>
                        <AMenuItem :leading-icon="IconDelete" @select="markAllDelete">
                            Mark all on this page to delete
                        </AMenuItem>
                        <AMenuItem :leading-icon="IconRestart" @select="unmarkAllDeletes">
                            Unmark all deletes
                        </AMenuItem>
                    </AMenu>
                    <AButton
                        :disabled="edits.size === 0"
                        focusable-when-disabled
                        :loading="mSave.isPending.value"
                        @click="mSave.mutate()"
                    >
                        Save
                    </AButton>
                </div>
            </div>

            <QueryError :mutation="mSave" closable />
            <QueryError :query="qBrokenRefs" />

            <ACard padding="none">
                <ATable :aria-busy="qBrokenRefs.isFetching.value">
                    <thead>
                        <tr>
                            <th>Broken ref</th>
                            <th>Data</th>
                            <th class="w-1/2">Action</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr v-if="qBrokenRefs.isLoading.value">
                            <td colspan="3"><ASpinner class="mx-auto flex" /></td>
                        </tr>
                        <tr v-else-if="qBrokenRefs.isSuccess.value && !items.length">
                            <td colspan="3" class="text-fg-muted text-center">
                                {{ search ? 'No refs match the search.' : 'No broken references.' }}
                            </td>
                        </tr>
                        <tr v-for="item in items" :key="item.id">
                            <td class="min-w-48 py-2">
                                <code
                                    class="text-[13px] [overflow-wrap:anywhere]"
                                    :class="{ 'line-through': edits.get(item.id) === 'delete' }"
                                    >{{ item.uri }}</code
                                >
                            </td>
                            <td class="min-w-36 py-2">
                                <div class="flex items-center gap-1">
                                    <span class="text-fg-muted">{{ summarize(item) }}</span>
                                    <AIconButton
                                        :icon="IconEye"
                                        :label="`View the data for ${item.uri}`"
                                        size="sm"
                                        @click="showBrokenRefDetailModal(item)"
                                    />
                                </div>
                            </td>
                            <td class="py-2">
                                <div class="flex items-start gap-1">
                                    <ACombobox
                                        :model-value="replacement(item.id)"
                                        :options="contentUris"
                                        :label="`New ref for ${item.uri}`"
                                        placeholder="Replace with…"
                                        size="sm"
                                        clearable
                                        :disabled="edits.get(item.id) === 'delete'"
                                        :hint="actionHint(item.id)"
                                        hint-tone="warning"
                                        class="min-w-64 flex-1"
                                        @update:model-value="v => setEdit(item.id, v ?? undefined)"
                                    />
                                    <AIconButton
                                        :icon="IconDelete"
                                        :label="`Delete the data for ${item.uri}`"
                                        size="sm"
                                        :pressed="edits.get(item.id) === 'delete'"
                                        class="mt-1"
                                        @click="toggleDelete(item.id)"
                                    />
                                </div>
                            </td>
                        </tr>
                    </tbody>
                </ATable>
                <footer
                    v-if="pageCount > 1"
                    class="border-outline-variant flex justify-center border-t px-4 py-2"
                >
                    <APagination v-model:page="page" :length="pageCount" label="Broken ref pages" />
                </footer>
            </ACard>
        </template>
    </div>
</template>

<script setup lang="ts">
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { useHead } from '@unhead/vue'
import { refDebounced } from '@vueuse/core'
import { computed, nextTick, reactive, ref, useTemplateRef, watch } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import ACombobox from '@/ui/ACombobox.vue'
import AIconButton from '@/ui/AIconButton.vue'
import AMenu from '@/ui/AMenu.vue'
import AMenuItem from '@/ui/AMenuItem.vue'
import APageHeader from '@/ui/APageHeader.vue'
import APagination from '@/ui/APagination.vue'
import ASelect from '@/ui/ASelect.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ATable from '@/ui/ATable.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconChevronDown, IconDelete, IconEye, IconMagnify, IconRestart } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'
import { librariesApi } from '@/utils/api/libraries'
import { READING_STATUS_LABELS } from '@/utils/api/types'
import type { BrokenUserToContent } from '@/utils/api/types'
import { plural } from '@/utils/misc'
import { showBrokenRefDetailModal } from './BrokenRefDetailModal.vue'

const PAGE_SIZE = 50

useHead({ title: 'Broken references' })

const queryClient = useQueryClient()
const toast = useToast()
const emptyHeading = useTemplateRef('emptyHeading')
const qSummary = contentApi.useBrokenRefsSummary()
const qLibraries = librariesApi.useList()

const selectedLibraryId = ref<string | null>(null)
const searchInput = ref('')
const search = refDebounced(searchInput, 300)
const page = ref(1)
const edits = reactive(new Map<string, string | 'delete'>())

const libraryOptions = computed(() => {
    const summaryData = qSummary.data.value ?? []
    const libraries = qLibraries.data.value ?? []
    // Refs whose library was deleted have no library to fix them in.
    return summaryData.flatMap(s => {
        if (!s.library_id) return []
        const lib = libraries.find(l => l.id === s.library_id)
        return [{ value: s.library_id, label: `${lib?.name ?? s.library_id} (${s.count})` }]
    })
})

watch(
    libraryOptions,
    options => {
        if (!options.some(o => o.value === selectedLibraryId.value)) {
            selectedLibraryId.value = options[0]?.value ?? null
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
const contentUris = computed(() => qUris.data.value?.content_uris ?? [])
const items = computed(() => qBrokenRefs.data.value?.data ?? [])
const pageCount = computed(() => Math.ceil((qBrokenRefs.data.value?.total ?? 0) / PAGE_SIZE))

// Saving can shrink the list below the current page, also when the pagination isn't rendered.
watch(
    () => qBrokenRefs.data.value?.total,
    total => {
        if (total == null) return
        page.value = Math.min(page.value, Math.max(1, Math.ceil(total / PAGE_SIZE)))
    }
)

watch([selectedLibraryId, search], () => {
    edits.clear()
    page.value = 1
})

function replacement(id: string): string | null {
    const edit = edits.get(id)
    return edit && edit !== 'delete' ? edit : null
}

function setEdit(id: string, uri: string | undefined) {
    if (uri) {
        edits.set(id, uri)
    } else {
        edits.delete(id)
    }
}

function markAllDelete() {
    for (const item of items.value) {
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

// Shown under the field, not in a tooltip, so touch users see it too.
function actionHint(id: string) {
    if (edits.get(id) === 'delete') return 'Saving deletes this data.'
    const edit = replacement(id)
    if (edit && userUriSet.value.has(edit)) {
        return 'This ref already has user data. Saving replaces it with this older entry.'
    }
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
    onSuccess: async () => {
        toast.show({ message: `Saved ${plural(edits.size, 'change', 'changes')}` })
        edits.clear()
        await Promise.all([
            queryClient.invalidateQueries({ queryKey: ['content', 'broken-refs'] }),
            queryClient.invalidateQueries({ queryKey: ['content', 'broken-refs-summary'] }),
        ])
        // Saving the last refs removes the table, and the focused button with it.
        await nextTick()
        emptyHeading.value?.focus()
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
