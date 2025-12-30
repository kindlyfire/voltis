<template>
    <VContainer v-if="qContent.error.value">
        <AQueryError :query="qContent" />
    </VContainer>
    <div v-else-if="!contentType" class="absolute inset-0 flex items-center justify-center">
        <VProgressCircular indeterminate size="64" />
    </div>
    <template v-else>
        <ComicDisplay v-if="contentType === 'comic'" :contentId="contentId" />
        <BookDisplay v-else-if="contentType === 'book'" :content-id="contentId" />
    </template>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { contentApi } from '@/utils/api/content'
import ComicDisplay from '../read/ComicDisplay/ComicDisplay.vue'
import BookDisplay from '../read/BookDisplay/BookDisplay.vue'
import { useHead } from '@unhead/vue'
import type { ContentType } from '@/utils/api/types'
import AQueryError from '@/components/AQueryError.vue'

const route = useRoute()
const router = useRouter()
const contentId = computed(() => route.params.id as string)
const qContent = contentApi.useGet(contentId)

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
        return qContent.data.value?.title ?? null
    },
})
</script>
