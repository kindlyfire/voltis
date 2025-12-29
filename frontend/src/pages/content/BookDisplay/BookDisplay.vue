<template>
    <BookReader v-if="store.chapterHref" />
    <BookInfo v-else />
</template>

<script setup lang="ts">
import { watch } from 'vue'
import { useRoute } from 'vue-router'
import BookInfo from './BookInfo.vue'
import BookReader from './BookReader.vue'
import { useBookDisplayStore } from './useBookDisplayStore'
import { useHead } from '@unhead/vue'

const props = defineProps<{
    contentId: string
}>()

const route = useRoute()
const store = useBookDisplayStore()

watch(
    () => props.contentId,
    newContentId => {
        store.setContentId(newContentId)
    },
    { immediate: true }
)

watch(
    () => route.query.ch,
    newChapterHref => {
        store.setChapterHref(typeof newChapterHref === 'string' ? newChapterHref : null)
    },
    { immediate: true }
)

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
