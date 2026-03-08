<template>
    <VContainer>
        <div class="mb-4 flex items-center">
            <h1 class="text-h5">My Lists</h1>
            <div class="grow" />
            <VBtn color="primary" @click="showListModal('new')">Create List</VBtn>
        </div>

        <div class="grid gap-4 lg:grid-cols-3">
            <div v-for="list in lists.data?.value" :key="list.id">
                <VCard :to="`/${list.id}`">
                    <div class="flex">
                        <img
                            v-if="randomCover(list.cover_content_ids)"
                            :src="randomCover(list.cover_content_ids)!"
                            class="aspect-[2.1/3] w-[120px] object-cover"
                        />
                        <div
                            v-else
                            class="bg-surface-variant flex aspect-[2.1/3] w-[120px] shrink-0 items-center justify-center rounded-s"
                        >
                            <VIcon icon="mdi-playlist-play" size="40" />
                        </div>
                        <div class="flex grow flex-col p-4">
                            <div class="mb-1 text-lg font-medium">{{ list.name }}</div>
                            <div class="text-sm opacity-60">
                                {{ list.entry_count ?? 0 }} entries
                            </div>
                            <div class="text-sm capitalize opacity-60">
                                {{ list.visibility }}
                            </div>
                            <div class="mt-auto text-sm opacity-60">
                                {{ new Date(list.updated_at).toLocaleDateString() }}
                            </div>
                        </div>
                        <div class="flex items-start p-2">
                            <VBtn
                                icon="mdi-pencil"
                                variant="text"
                                size="small"
                                @click.prevent="showListModal(list.id)"
                            />
                        </div>
                    </div>
                </VCard>
            </div>
        </div>
    </VContainer>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { customListsApi } from '@/utils/api/custom-lists'
import { API_URL } from '@/utils/fetch'
import { showListModal } from './ListModal.vue'

useHead({
    title: 'Lists',
})

const lists = customListsApi.useList('me')

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
