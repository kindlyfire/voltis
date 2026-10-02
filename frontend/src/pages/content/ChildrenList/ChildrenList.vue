<template>
    <section v-if="isSeries" class="flex flex-col gap-2" aria-labelledby="children-heading">
        <h2 id="children-heading" class="font-display text-[30px] font-semibold">Contents</h2>
        <ContentGrid
            :params="{ parent_id: content.id, sort: 'order', sort_order: 'asc' }"
            :parent="content"
            to-read-route
        />
    </section>
    <BookChaptersList v-else-if="content.type === 'book'" :content="content" />
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import ContentGrid from '@/components/ContentGrid/ContentGrid.vue'
import type { Content } from '@/utils/api/types'
import BookChaptersList from './BookChaptersList.vue'

const props = defineProps<{
    content: Content
}>()

const isSeries = computed(() => props.content.type.includes('series'))
</script>
