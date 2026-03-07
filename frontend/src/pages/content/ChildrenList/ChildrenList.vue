<template>
    <VContainer>
        <AContentGrid
            v-if="isSeries"
            :params="{ parent_id: content.id, sort: 'order', sort_order: 'asc' }"
            to-read-route
        />
        <BookChaptersList v-else-if="content.type === 'book'" :content="content" />
    </VContainer>
</template>

<script lang="ts" setup>
import AContentGrid from '@/components/AContentGrid.vue'
import type { Content } from '@/utils/api/types'
import { computed } from 'vue'
import BookChaptersList from './BookChaptersList.vue'

const props = defineProps<{
    content: Content
}>()

const isSeries = computed(() => props.content.type.includes('series'))
</script>
