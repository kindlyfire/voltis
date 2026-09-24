<template>
    <ADialog :open="open" title="Search MangaBaka" size="lg" @update:open="v => !v && close()">
        <div class="flex flex-col gap-4">
            <ATextField
                v-model="searchInput"
                label="Title or MangaBaka URL"
                type="search"
                :leading-icon="IconMagnify"
                :loading="qSearch.isFetching.value"
                autofocus
                @keydown.enter="!$event.isComposing && $event.keyCode !== 229 && commitSearch()"
            />

            <QueryError :query="qSearch" />

            <div
                v-if="qSearch.isFetching.value && !searchResults.length"
                class="flex justify-center py-10"
            >
                <ASpinner label="Searching" />
            </div>

            <ul v-else-if="searchResults.length" class="flex flex-col gap-2" aria-label="Results">
                <li v-for="item in searchResults" :key="item.id">
                    <ACard variant="tonal" padding="md" class="flex-row flex-wrap gap-3">
                        <ACover :src="item.cover_url" alt="" class="w-16 shrink-0 self-start" />
                        <div class="flex min-w-0 flex-1 flex-col gap-1.5">
                            <h3 :id="`mb-${item.id}`" class="text-base font-medium">
                                {{ item.title }}
                            </h3>
                            <div class="flex flex-wrap gap-1.5">
                                <AChip size="sm">{{ item.type }}</AChip>
                                <AChip v-if="item.status" size="sm">{{ item.status }}</AChip>
                                <AChip v-if="item.year" size="sm">{{ item.year }}</AChip>
                            </div>
                            <p v-if="item.authors.length" class="text-fg-muted text-xs">
                                {{ item.authors.join(', ') }}
                            </p>
                            <p v-if="item.genres.length" class="text-fg-muted text-xs">
                                {{ item.genres.join(', ') }}
                            </p>
                        </div>
                        <div
                            class="flex shrink-0 justify-end gap-1 max-sm:w-full sm:flex-col sm:items-stretch"
                        >
                            <AButton
                                size="sm"
                                :loading="
                                    mLink.isPending.value && mLink.variables.value?.id === item.id
                                "
                                :disabled="
                                    mLink.isPending.value && mLink.variables.value?.id !== item.id
                                "
                                :aria-describedby="`mb-${item.id}`"
                                @click="mLink.mutate(item)"
                            >
                                Select
                            </AButton>
                            <AButton
                                size="sm"
                                variant="text"
                                :href="`https://mangabaka.org/${item.id}`"
                                :aria-describedby="`mb-${item.id}`"
                            >
                                Open
                            </AButton>
                        </div>
                    </ACard>
                </li>
            </ul>

            <p v-else-if="qSearch.isSuccess.value" class="text-fg-muted py-4 text-center">
                No results found.
            </p>

            <QueryError :mutation="mLink" />
        </div>
        <template #actions>
            <AButton variant="text" tone="neutral" @click="close()">Cancel</AButton>
        </template>
    </ADialog>
</template>

<script setup lang="ts">
import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import QueryError from '@/components/QueryError.vue'
import AButton from '@/ui/AButton.vue'
import ACard from '@/ui/ACard.vue'
import AChip from '@/ui/AChip.vue'
import ACover from '@/ui/ACover.vue'
import ADialog from '@/ui/ADialog.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ATextField from '@/ui/ATextField.vue'
import { IconMagnify } from '@/ui/icons'
import { useToast } from '@/ui/useToast'
import { contentApi } from '@/utils/api/content'
import { metadataSourcesApi } from '@/utils/api/metadata-sources'
import type { MangaBakaSearchResult } from '@/utils/api/types'

const props = defineProps<{
    open: boolean
    close: () => void
    contentId: string
}>()

const queryClient = useQueryClient()
const toast = useToast()
const qContent = contentApi.useGet(() => props.contentId)

const searchInput = ref('')
const committedQuery = ref<string | null>(null)

let debounceTimer: ReturnType<typeof setTimeout> | undefined
onBeforeUnmount(() => clearTimeout(debounceTimer))
const mangabakaUrlRe = /mangabaka\.org\/manga\/(\d+)/

function getSearchType(): 'comic' | 'book' {
    const t = qContent.data?.value?.type
    return t === 'book_series' ? 'book' : 'comic'
}

function resolveQuery(input: string): string {
    const m = mangabakaUrlRe.exec(input)
    return m?.[1] ?? input
}

function commitSearch() {
    clearTimeout(debounceTimer)
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
    clearTimeout(debounceTimer)
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
    onSuccess(_data, item) {
        toast.show({ message: `Linked to ${item.title} on MangaBaka` })
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
