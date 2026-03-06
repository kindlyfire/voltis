<template>
    <VContainer>
        <div class="flex items-center mb-4">
            <h1 class="text-h5">My Lists</h1>
            <div class="grow" />
            <VBtn color="primary" @click="showListModal('new')">Create List</VBtn>
        </div>

        <div class="grid lg:grid-cols-3 gap-4">
            <div v-for="list in lists.data?.value" :key="list.id">
                <VCard :to="`/${list.id}`">
                    <div class="flex">
                        <img
                            v-if="randomCover(list.cover_content_ids)"
                            :src="randomCover(list.cover_content_ids)!"
                            class="object-cover aspect-[2.1/3] w-[120px]"
                        />
                        <div
                            v-else
                            class="flex items-center justify-center shrink-0 rounded-s bg-surface-variant w-[120px] aspect-[2.1/3]"
                        >
                            <VIcon icon="mdi-playlist-play" size="40" />
                        </div>
                        <div class="p-4 flex flex-col grow">
                            <div class="text-lg font-medium mb-1">{{ list.name }}</div>
                            <div class="text-sm opacity-60">
                                {{ list.entry_count ?? 0 }} entries
                            </div>
                            <div class="text-sm opacity-60 capitalize">
                                {{ list.visibility }}
                            </div>
                            <div class="text-sm opacity-60 mt-auto">
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
import { showListModal } from './ListModal.vue'
import { API_URL } from '@/utils/fetch'

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
