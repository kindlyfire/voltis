<template>
    <BookReader :content-id="contentId" />
</template>

<script setup lang="ts">
import BookReader from './BookReader.vue'
import { useBookDisplayStore } from './useBookDisplayStore'
import { useHead } from '@unhead/vue'

const props = defineProps<{
    contentId: string
}>()

const store = useBookDisplayStore()

useHead({
    title() {
        if (!store.content) return 'Loading...'
        let text = store.content.title

        const currentChapter = store.chapters?.find(ch => ch.href === store.chapterHref)
        if (currentChapter) {
            text = `${currentChapter.title || currentChapter.id} • ${text}`
        }

        return text
    },
})
</script>
