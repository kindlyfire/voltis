<template>
    <VContainer>
        <div class="d-flex align-center mb-4">
            <h1 class="text-h5">My Lists</h1>
            <VSpacer />
            <VBtn color="primary" @click="showListModal('new')">Create List</VBtn>
        </div>

        <VRow>
            <VCol v-for="list in lists.data?.value" :key="list.id" cols="12" sm="6" md="4">
                <VCard :to="`/${list.id}`" hover>
                    <div class="d-flex">
                        <img
                            v-if="randomCover(list.cover_content_ids)"
                            :src="randomCover(list.cover_content_ids)!"
                            class="object-cover w-[120px] h-[140px]"
                        />
                        <div
                            v-else
                            class="d-flex align-center justify-center shrink-0 rounded-s bg-surface-variant w-[120px] h-[140px]"
                        >
                            <VIcon icon="mdi-playlist-play" size="40" />
                        </div>
                        <div class="pa-4 d-flex flex-column justify-center grow">
                            <div class="text-h6 mb-1">{{ list.name }}</div>
                            <div class="text-body-2 text-medium-emphasis">
                                {{ list.entry_count ?? 0 }} entries
                            </div>
                            <div class="text-caption text-medium-emphasis text-capitalize">
                                {{ list.visibility }}
                            </div>
                            <div class="text-caption text-medium-emphasis mt-1">
                                {{ new Date(list.updated_at).toLocaleDateString() }}
                            </div>
                        </div>
                        <div class="d-flex align-start pa-2">
                            <VBtn
                                icon="mdi-pencil"
                                variant="text"
                                size="small"
                                @click.prevent="showListModal(list.id)"
                            />
                        </div>
                    </div>
                </VCard>
            </VCol>
        </VRow>
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
