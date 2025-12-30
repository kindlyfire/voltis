<template>
    <VContainer>
        <AContentGrid
            v-if="isSeries"
            :items="children ?? []"
            :loading="qChildren.isLoading.value"
            to-read-route
        />
        <BookChaptersList v-else-if="content.type === 'book'" :content="content" />
    </VContainer>
</template>

<script lang="ts" setup>
import AContentGrid from '@/components/AContentGrid.vue'
import { contentApi } from '@/utils/api/content'
import type { Content } from '@/utils/api/types'
import { computed } from 'vue'
import BookChaptersList from './BookChaptersList.vue'

const props = defineProps<{
    content: Content
}>()

const isSeries = computed(() => props.content.type.includes('series'))

const qChildren = contentApi.useList(() =>
    isSeries.value
        ? {
              parent_id: props.content.id,
              sort: 'order',
              sort_order: 'asc',
          }
        : undefined
)
const children = computed(() => qChildren.data.value?.data)
</script>
