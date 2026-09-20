<template>
    <BookReader :content-id="contentId" />
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { onUnmounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import { parseBookEntry } from './bookEntry'
import BookReader from './BookReader.vue'
import { useBookDisplayStore } from './useBookDisplayStore'

const props = defineProps<{
    contentId: string
}>()

const route = useRoute()
const store = useBookDisplayStore()

watch(
    () => [props.contentId, route.query.ch, route.query.frag, route.query.page],
    () => {
        store.setContent(props.contentId, parseBookEntry(route.query))
    },
    { immediate: true }
)

onUnmounted(() => {
    store.dispose()
})

useHead({
    title() {
        const session = store.session
        if (!session?.content) return 'Loading...'
        return session.title ? `${session.title} • ${session.content.title}` : session.content.title
    },
})
</script>
