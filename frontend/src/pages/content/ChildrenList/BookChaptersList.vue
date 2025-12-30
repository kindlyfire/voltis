<template>
    <VContainer>
        <h2 class="text-h5 mb-4">Chapters</h2>
        <VList v-if="visibleChapters.items.length">
            <VListItem
                v-for="(chapter, index) in visibleChapters.items"
                :key="chapter.id"
                :to="`/r/${content.id}?ch=${encodeURIComponent(chapter.href)}`"
                class="border-b"
            >
                <template #prepend>
                    <span class="text-medium-emphasis mr-4">{{ index + 1 }}</span>
                </template>
                <VListItemTitle>{{ chapter.title || chapter.id }}</VListItemTitle>
            </VListItem>
        </VList>
        <div v-else-if="qChapters.isLoading.value" class="d-flex justify-center py-8">
            <VProgressCircular indeterminate />
        </div>
        <AQueryError v-else-if="qChapters.error.value" :query="qChapters" />
        <VCheckbox
            v-if="visibleChapters.hasHidden"
            :model-value="settings.showHidden"
            @update:model-value="settings.setShowHidden(toValue($event) ?? false)"
            label="Show hidden chapters"
            density="compact"
            hide-details
            class="mt-2"
        />
    </VContainer>
</template>

<script setup lang="ts">
import type { Content } from '@/utils/api/types'
import {
    useBookDisplaySettings,
    useVisibleBookChapters as useVisibleBookChapters,
} from '../../read/BookDisplay/useBookDisplayStore'
import AQueryError from '@/components/AQueryError.vue'
import { contentApi } from '@/utils/api/content'
import { computed, toValue } from 'vue'

const props = defineProps<{
    content: Content
}>()

const settings = useBookDisplaySettings(() => props.content.id)

const qChapters = contentApi.useBookChapters(() => props.content.id)
const visibleChapters = useVisibleBookChapters(
    computed(() => props.content.id),
    qChapters.data
)
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
