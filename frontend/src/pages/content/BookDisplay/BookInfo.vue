<template>
    <VContainer>
        <div class="d-flex gap-6 mb-6">
            <div class="w-[200px] shrink-0">
                <VImg
                    v-if="store.content?.cover_uri"
                    :src="`${API_URL}/files/cover/${store.content.id}?v=${store.content.file_mtime}`"
                    :aspect-ratio="2 / 3"
                    cover
                    class="rounded"
                />
            </div>
            <div>
                <h1 class="text-h4 mb-2">{{ store.content?.title }}</h1>

                <dl v-if="store.content?.meta" class="metadata-list">
                    <template v-if="store.content.meta.authors?.length">
                        <dt>Author{{ store.content.meta.authors.length > 1 ? 's' : '' }}</dt>
                        <dd>{{ store.content.meta.authors.join(', ') }}</dd>
                    </template>
                    <template v-if="store.content.meta.publisher">
                        <dt>Publisher</dt>
                        <dd>{{ store.content.meta.publisher }}</dd>
                    </template>
                    <template v-if="store.content.meta.publication_date">
                        <dt>Published</dt>
                        <dd>{{ store.content.meta.publication_date }}</dd>
                    </template>
                    <template v-if="store.content.meta.language">
                        <dt>Language</dt>
                        <dd>{{ store.content.meta.language }}</dd>
                    </template>
                </dl>

                <div v-if="store.content?.meta?.description" class="mt-4 max-w-prose">
                    <p class="text-body-2 description-text">{{ displayDescription }}</p>
                    <button
                        v-if="isDescriptionTruncated"
                        class="text-xs! text-blue-400!"
                        @click="showFullDescription = !showFullDescription"
                    >
                        {{ showFullDescription ? 'Show less' : 'Show more' }}
                    </button>
                </div>
            </div>
        </div>

        <h2 class="text-h5 mb-4">Chapters</h2>
        <VList v-if="store.visibleChapters.length">
            <VListItem
                v-for="(chapter, index) in store.visibleChapters"
                :key="chapter.id"
                :to="{ query: { ch: chapter.href } }"
                class="border-b"
            >
                <template #prepend>
                    <span class="text-medium-emphasis mr-4">{{ index + 1 }}</span>
                </template>
                <VListItemTitle>{{ chapter.title || chapter.id }}</VListItemTitle>
            </VListItem>
        </VList>
        <div v-else-if="store.qChapters.isLoading" class="d-flex justify-center py-8">
            <VProgressCircular indeterminate />
        </div>
        <AQueryError v-else-if="store.qChapters.error" :query="store.qChapters" />
        <VCheckbox
            v-if="store.hasHiddenChapters"
            :model-value="store.showHiddenChapters"
            @update:model-value="store.setShowHidden(store.contentId!, $event ?? false)"
            label="Show hidden chapters"
            density="compact"
            hide-details
            class="mt-2"
        />
    </VContainer>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { API_URL } from '@/utils/fetch'
import { useBookDisplayStore } from './useBookDisplayStore'
import AQueryError from '@/components/AQueryError.vue'

const store = useBookDisplayStore()
const MAX_DESC_LENGTH = 400
const showFullDescription = ref(false)

const isDescriptionTruncated = computed(() => {
    const desc = store.content?.meta?.description
    return desc ? desc.length > MAX_DESC_LENGTH : false
})

const displayDescription = computed(() => {
    const desc = store.content?.meta?.description
    if (!desc) return ''
    if (showFullDescription.value || desc.length <= MAX_DESC_LENGTH) return desc

    // Find the last space before MAX_DESC_LENGTH to avoid splitting words
    const truncated = desc.slice(0, MAX_DESC_LENGTH)
    const lastSpace = truncated.lastIndexOf(' ')
    const cutoff = lastSpace > 0 ? lastSpace : MAX_DESC_LENGTH
    return desc.slice(0, cutoff) + '...'
})
</script>

<style scoped>
.metadata-list {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.25rem 1rem;
}

.metadata-list dt {
    color: rgba(var(--v-theme-on-surface), 0.6);
    font-size: 0.875rem;
}

.metadata-list dd {
    margin: 0;
    font-size: 0.875rem;
}

.description-text {
    white-space: pre-wrap;
    margin: 0;
}
</style>
