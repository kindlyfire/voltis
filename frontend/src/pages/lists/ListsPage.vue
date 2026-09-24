<template>
    <div class="page-frame flex flex-col gap-3.5">
        <div class="flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
            <APageHeader title="Lists" />
            <AButton :leading-icon="IconPlus" @click="showListModal('new')">Create list</AButton>
        </div>

        <QueryError :query="qLists" />

        <div v-if="qLists.isLoading.value" class="flex justify-center py-16">
            <ASpinner size="lg" />
        </div>

        <div
            v-else-if="qLists.isSuccess.value && !lists.length"
            class="text-fg-muted flex flex-col items-center gap-2 py-16 text-center"
        >
            <p class="text-fg font-display text-xl font-semibold">No lists yet</p>
            <p class="text-sm">Collect series and books into lists to keep track of them.</p>
        </div>

        <div v-else class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            <ACard
                v-for="list in lists"
                :key="list.id"
                :to="`/${list.id}`"
                :title="list.name"
                padding="md"
                class="grid grid-cols-[80px_1fr] content-start gap-x-4 gap-y-1 [&>header]:col-start-2"
            >
                <template #headerActions>
                    <AIconButton
                        :icon="IconPencil"
                        :label="`Edit ${list.name}`"
                        size="sm"
                        @click="showListModal(list.id)"
                    />
                </template>
                <ACover
                    :src="randomCover(list.cover_content_ids)"
                    alt=""
                    class="pointer-events-none col-start-1 row-span-2 row-start-1 self-start"
                />
                <dl class="text-fg-muted col-start-2 flex flex-col gap-0.5 text-sm">
                    <dt class="sr-only">Entries</dt>
                    <dd>{{ plural(list.entry_count ?? 0, 'entry', 'entries') }}</dd>
                    <dt class="sr-only">Visibility</dt>
                    <dd class="capitalize">{{ list.visibility }}</dd>
                    <dt class="sr-only">Updated</dt>
                    <dd>Updated {{ formatDate(list.updated_at) }}</dd>
                </dl>
            </ACard>
        </div>
    </div>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import ACover from '@/ui/ACover.vue'
import AIconButton from '@/ui/AIconButton.vue'
import APageHeader from '@/ui/APageHeader.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { IconPencil, IconPlus } from '@/ui/icons'
import { customListsApi } from '@/utils/api/custom-lists'
import { API_URL } from '@/utils/fetch'
import { plural } from '@/utils/misc'
import { showListModal } from './ListModal.vue'

useHead({
    title: 'Lists',
})

const qLists = customListsApi.useList('me')
const lists = computed(() => qLists.data.value ?? [])

function formatDate(value: string) {
    return new Date(value).toLocaleDateString()
}

const coverCache = new Map<string, string | null>()
function randomCover(ids: string[]): string | null {
    if (!ids.length) return null
    const key = ids.join(',')
    if (!coverCache.has(key)) {
        const id = ids[Math.floor(Math.random() * ids.length)]!
        coverCache.set(key, `${API_URL}/files/cover/${id}`)
    }
    return coverCache.get(key)!
}
</script>
