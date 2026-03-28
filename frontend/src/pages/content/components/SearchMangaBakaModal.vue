<template>
    <VDialog :model-value="open" @update:model-value="v => !v && close()" max-width="700">
        <VCard>
            <VCardTitle>Search MangaBaka</VCardTitle>
            <VCardText class="space-y-4!">
                <VTextField
                    v-model="searchInput"
                    variant="outlined"
                    placeholder="Search by title or paste a MangaBaka URL"
                    hide-details
                    autofocus
                    :loading="qSearch.isFetching.value"
                    @keydown.enter="commitSearch()"
                />

                <AQueryError :query="qSearch" />

                <div
                    v-if="qSearch.isFetching.value && !searchResults.length"
                    class="py-10 text-center"
                >
                    <VProgressCircular indeterminate />
                </div>

                <div v-else-if="searchResults.length" class="space-y-2!">
                    <VCard
                        v-for="item in searchResults"
                        :key="item.id"
                        variant="outlined"
                        class="d-flex ga-3 pa-3"
                    >
                        <img
                            v-if="item.cover_url"
                            :src="item.cover_url"
                            class="h-24 w-16 shrink-0 rounded object-cover"
                        />
                        <div v-else class="bg-surface-variant h-24 w-16 shrink-0 rounded" />
                        <div class="min-w-0 grow">
                            <div class="text-body-1 font-weight-medium">
                                {{ item.title }}
                            </div>
                            <div class="d-flex ga-2 mt-1 flex-wrap">
                                <VChip size="x-small" variant="tonal">
                                    {{ item.type }}
                                </VChip>
                                <VChip v-if="item.status" size="x-small" variant="tonal">
                                    {{ item.status }}
                                </VChip>
                                <VChip v-if="item.year" size="x-small" variant="tonal">
                                    {{ item.year }}
                                </VChip>
                            </div>
                            <div
                                v-if="item.authors.length"
                                class="text-caption text-medium-emphasis mt-1"
                            >
                                {{ item.authors.join(', ') }}
                            </div>
                            <div
                                v-if="item.genres.length"
                                class="text-caption text-medium-emphasis mt-1"
                            >
                                {{ item.genres.join(', ') }}
                            </div>
                        </div>
                        <div class="d-flex flex-column ga-1 shrink-0">
                            <VBtn
                                size="small"
                                color="primary"
                                variant="flat"
                                :loading="mLink.isPending.value"
                                :disabled="mLink.isPending.value"
                                @click="mLink.mutate(item)"
                            >
                                Select
                            </VBtn>
                            <VBtn
                                size="small"
                                variant="text"
                                :href="`https://mangabaka.org/${item.id}`"
                                target="_blank"
                                append-icon="mdi-open-in-new"
                            >
                                Open
                            </VBtn>
                        </div>
                    </VCard>
                </div>

                <div
                    v-else-if="qSearch.isSuccess.value"
                    class="text-medium-emphasis text-body-2 py-4 text-center"
                >
                    No results found.
                </div>

                <AQueryError :mutation="mLink" />
            </VCardText>
            <VCardActions>
                <VSpacer />
                <VBtn variant="text" @click="close()">Cancel</VBtn>
            </VCardActions>
        </VCard>
    </VDialog>
</template>

<script setup lang="ts">
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref, watch } from 'vue'
import AQueryError from '@/components/AQueryError.vue'
import { contentApi } from '@/utils/api/content'
import { metadataSourcesApi } from '@/utils/api/metadata-sources'
import type { MangaBakaSearchResult } from '@/utils/api/types'

const props = defineProps<{
    open: boolean
    close: () => void
    contentId: string
}>()

const queryClient = useQueryClient()
const qContent = contentApi.useGet(() => props.contentId)

const searchInput = ref('')
const committedQuery = ref<string | null>(null)

let debounceTimer: ReturnType<typeof setTimeout> | null = null
const mangabakaUrlRe = /mangabaka\.dev\/series\/(\d+)/

function getSearchType(): 'comic' | 'book' {
    const t = qContent.data?.value?.type
    return t === 'book_series' ? 'book' : 'comic'
}

function resolveQuery(input: string): string {
    const m = mangabakaUrlRe.exec(input)
    return m?.[1] ?? input
}

function commitSearch() {
    const q = searchInput.value.trim()
    if (q) committedQuery.value = resolveQuery(q)
}

// Pre-fill with title and trigger search when content loads
watch(
    () => qContent.data?.value?.title,
    title => {
        if (title && !searchInput.value) {
            searchInput.value = title
            commitSearch()
        }
    },
    { immediate: true }
)

// Debounced commit on input change
watch(searchInput, () => {
    if (debounceTimer) clearTimeout(debounceTimer)
    debounceTimer = setTimeout(() => commitSearch(), 400)
})

const qSearch = useQuery({
    queryKey: ['mangabaka-search', committedQuery, () => getSearchType()],
    queryFn: () => metadataSourcesApi.searchMangaBaka(committedQuery.value!, getSearchType()),
    enabled: computed(() => committedQuery.value != null),
})
const searchResults = computed(() => qSearch.data?.value?.data ?? [])

const mLink = useMutation({
    mutationFn: (item: MangaBakaSearchResult) =>
        metadataSourcesApi.linkMangaBaka(props.contentId, item.id),
    onSuccess() {
        queryClient.invalidateQueries({
            queryKey: ['content', 'metadata-layers', props.contentId],
        })
        queryClient.invalidateQueries({ queryKey: ['content', props.contentId] })
        props.close()
    },
})
</script>

<script lang="ts">
import { Modals } from '@/utils/modals'
import Self from './SearchMangaBakaModal.vue'

export function showSearchMangaBakaModal(contentId: string): Promise<void> {
    return Modals.show(Self, { contentId })
}
</script>
