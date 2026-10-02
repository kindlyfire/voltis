<template>
    <div v-if="qContent.error.value" class="nav:px-10 px-4 pt-3">
        <QueryError :query="qContent" />
    </div>
    <div v-else-if="!contentType" class="absolute inset-0 flex items-center justify-center">
        <ASpinner size="lg" />
    </div>
    <template v-else>
        <ComicDisplay v-if="contentType === 'comic'" :contentId="contentId" />
        <BookDisplay v-else-if="contentType === 'book'" :content-id="contentId" />
    </template>
</template>

<script setup lang="ts">
import { useHead } from '@unhead/vue'
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import QueryError from '@/components/QueryError.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { contentApi } from '@/utils/api/content'
import type { ContentType } from '@/utils/api/types'
import { readerTitle } from '@/utils/seriesItem'
import BookDisplay from '../read/BookDisplay/BookDisplay.vue'
import ComicDisplay from '../read/ComicDisplay/ComicDisplay.vue'

const route = useRoute()
const router = useRouter()
const contentId = computed(() => route.params.id as string)
const qContent = contentApi.useGet(contentId)
const qParent = contentApi.useGet(() => qContent.data.value?.parent_id || undefined)

// We cache the content type to avoid flickering when navigating between
// contents.
const contentType = ref(null as null | ContentType)
watch(
    () => qContent.data.value,
    newContent => {
        if (newContent) {
            contentType.value = newContent.type
        }
    },
    { immediate: true }
)

// Redirect series to the info page
watch(
    () => qContent.data.value,
    newContent => {
        if (newContent && newContent.type.includes('series')) {
            router.replace('/' + newContent.id)
        }
    },
    { immediate: true }
)

useHead({
    title() {
        const c = qContent.data.value
        return c ? readerTitle(c, qParent.data.value) : null
    },
})
</script>
