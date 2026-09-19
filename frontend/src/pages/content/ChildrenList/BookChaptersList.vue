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
            v-model="showHidden"
            label="Show hidden chapters"
            density="compact"
            hide-details
            class="mt-2"
        />
    </VContainer>
</template>

<script setup lang="ts">
import AQueryError from '@/components/AQueryError.vue'
import { contentApi } from '@/utils/api/content'
import type { Content } from '@/utils/api/types'
import {
    useBookShowHidden,
    useVisibleBookChapters,
} from '../../read/BookDisplay/useBookDisplayStore'

const props = defineProps<{
    content: Content
}>()

const showHidden = useBookShowHidden(() => props.content.id)

const qChapters = contentApi.useBookChapters(() => props.content.id)
const visibleChapters = useVisibleBookChapters(() => props.content.id, qChapters.data)
</script>
