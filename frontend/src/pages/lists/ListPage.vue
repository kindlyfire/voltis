<template>
    <!-- In the card header on wide screens, below the chips on narrow ones. -->
    <DefineEntryActions v-slot="{ entry, idx }">
        <AIconButton
            :id="`move-up-${entry.id}`"
            :icon="IconChevronUp"
            label="Move up"
            size="sm"
            :disabled="idx === 0 || mReorderEntries.isPending.value"
            focusable-when-disabled
            @click="moveEntry(entry.id, -1)"
        />
        <AIconButton
            :id="`move-down-${entry.id}`"
            :icon="IconChevronDown"
            label="Move down"
            size="sm"
            :disabled="idx === entries.length - 1 || mReorderEntries.isPending.value"
            focusable-when-disabled
            @click="moveEntry(entry.id, 1)"
        />
        <AIconButton
            :id="`remove-${entry.id}`"
            :icon="IconDelete"
            label="Remove from list"
            tone="danger"
            size="sm"
            :loading="
                mDeleteEntry.isPending.value && mDeleteEntry.variables.value?.entryId === entry.id
            "
            @click="handleDelete(entry)"
        />
    </DefineEntryActions>

    <div class="page-frame flex max-w-[960px] flex-col gap-3.5">
        <QueryError :query="qList" />

        <div v-if="qList.isLoading.value" class="flex justify-center py-16">
            <ASpinner size="lg" />
        </div>

        <template v-else-if="list">
            <APageHeader :title="list.name">
                <template #actions>
                    <AIconButton
                        :icon="IconPencil"
                        label="Edit list"
                        @click="showListModal(list.id)"
                    />
                </template>
                <template #meta>
                    <span class="capitalize">{{ list.visibility }}</span>
                    · {{ plural(list.entry_count ?? 0, 'entry', 'entries') }} · Updated
                    {{ formatDate(list.updated_at) }}
                </template>
            </APageHeader>

            <p v-if="list.description" class="text-fg-muted text-sm whitespace-pre-wrap">
                {{ list.description }}
            </p>

            <QueryError :mutation="mReorderEntries" />
            <QueryError :mutation="mDeleteEntry" />

            <ol v-if="entries.length" class="mt-2 flex flex-col gap-3">
                <template v-for="(entry, idx) in entries" :key="entry.id">
                    <li v-if="entry.content">
                        <ACard
                            :to="`/${entry.content.id}`"
                            :title="entryTitle(entry)"
                            padding="md"
                            class="grid grid-cols-[72px_1fr] content-start gap-x-4 gap-y-2 sm:grid-cols-[88px_1fr] [&>header]:col-start-2"
                        >
                            <template #headerActions>
                                <AIconButton
                                    :icon="IconPencil"
                                    label="Edit notes"
                                    size="sm"
                                    @click="
                                        showEntryModal({
                                            listId: list.id,
                                            entryId: entry.id,
                                            title: entryTitle(entry),
                                            notes: entry.notes,
                                        })
                                    "
                                />
                                <ReuseEntryActions v-if="wide" :entry="entry" :idx="idx" />
                            </template>

                            <ACover
                                :src="entryCoverUri(entry)"
                                alt=""
                                class="pointer-events-none col-start-1 row-span-2 row-start-1 self-start"
                            />
                            <div class="col-start-2 flex min-w-0 flex-col gap-3">
                                <div class="flex flex-wrap gap-1.5">
                                    <AChip size="sm">
                                        {{ displayContentType(entry.content.type) }}
                                    </AChip>
                                    <AChip size="sm" :to="`/${entry.library_id}`">
                                        {{ libraryName(entry.library_id) }}
                                    </AChip>
                                </div>
                                <div v-if="!wide" class="-my-1 -ml-2 flex gap-1">
                                    <ReuseEntryActions :entry="entry" :idx="idx" />
                                </div>
                                <div v-if="entry.notes">
                                    <h3 class="text-fg-muted text-xs font-semibold">Notes</h3>
                                    <p class="text-sm whitespace-pre-wrap">{{ entry.notes }}</p>
                                </div>
                            </div>
                        </ACard>
                    </li>
                </template>
            </ol>
            <p v-else class="text-fg-muted py-12 text-center text-sm">
                No entries yet. Add series or books from their page, or from a library in select
                mode.
            </p>
        </template>
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { createReusableTemplate, until, useMediaQuery } from '@vueuse/core'
import { computed, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import QueryError from '@/components/QueryError.vue'
import ACard from '@/ui/ACard.vue'
import AChip from '@/ui/AChip.vue'
import ACover from '@/ui/ACover.vue'
import AIconButton from '@/ui/AIconButton.vue'
import APageHeader from '@/ui/APageHeader.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { IconChevronDown, IconChevronUp, IconDelete, IconPencil } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { coverUrl } from '@/utils/api/content'
import { customListsApi } from '@/utils/api/custom-lists'
import { librariesApi } from '@/utils/api/libraries'
import type { CustomListEntry } from '@/utils/api/types'
import { displayContentType, plural } from '@/utils/misc'
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
const toast = useToast()
const wide = useMediaQuery('(min-width: 40rem)')
const [DefineEntryActions, ReuseEntryActions] = createReusableTemplate<{
    entry: CustomListEntry
    idx: number
}>()

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

function entryTitle(entry: CustomListEntry) {
    return entry.content?.title || entry.uri
}

function entryCoverUri(entry: CustomListEntry) {
    return entry.content && coverUrl(entry.content)
}

async function handleDelete(entry: CustomListEntry) {
    // Focus moves to the next entry's remove button, else the previous one's, else the heading.
    const visible = entries.value.filter(e => e.content)
    const idx = visible.findIndex(e => e.id === entry.id)
    const neighbour = visible[idx + 1] ?? visible[idx - 1]
    await mDeleteEntry.mutateAsync({ listId: listId.value, entryId: entry.id })
    toast.show({ message: `Removed ${entryTitle(entry)} from the list` })
    await until(qList.isFetching).toBe(false)
    await nextTick()
    const target =
        (neighbour && document.getElementById(`remove-${neighbour.id}`)) ??
        document.querySelector<HTMLElement>('#main h1')
    if (target?.tagName === 'H1') target.tabIndex = -1
    target?.focus()
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
    await until(qList.isFetching).toBe(false)
    // Re-rendering in the new order can drop focus from the pressed button.
    await nextTick()
    const button = `move-${delta < 0 ? 'up' : 'down'}-${entryId}`
    if (document.activeElement === document.body) document.getElementById(button)?.focus()
}
</script>
