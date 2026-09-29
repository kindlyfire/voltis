<template>
    <BookReader :content-id="contentId" />
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { onUnmounted, watch } from 'vue'
import { onBeforeRouteLeave, useRoute } from 'vue-router'
import { useLayoutStore } from '@/pages/_layout/useLayoutStore'
import { trackRecentlyReadWrite } from '@/utils/api/content'
import { parseBookEntry } from './bookEntry'
import BookReader from './BookReader.vue'
import { setReaderDark } from './prepareDocument'
import { useBookDisplayStore } from './useBookDisplayStore'

const props = defineProps<{
    contentId: string
}>()

const route = useRoute()
const store = useBookDisplayStore()
const layout = useLayoutStore()
layout.navbarHidden.useLayer('bookReader', true)
layout.sidebarTemporary.useLayer('bookReader', true)
watch(
    () => layout.theme,
    theme => setReaderDark(theme === 'dark'),
    { immediate: true }
)

watch(
    () => [props.contentId, route.query.ch, route.query.frag, route.query.page],
    () => {
        store.setContent(props.contentId, parseBookEntry(route.query))
    },
    { immediate: true }
)

// Not in dispose: unmounting runs after the next page has rendered the old order.
onBeforeRouteLeave(() => {
    trackRecentlyReadWrite(store.session?.leave())
})
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
