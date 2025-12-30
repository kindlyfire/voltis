<template>
    <div class="book-reader">
        <VAppBar density="compact" class="book-reader-toolbar">
            <VBtn icon :to="`/${props.contentId}`" variant="text" exact>
                <VIcon>mdi-arrow-left</VIcon>
            </VBtn>
            <VAppBarTitle class="text-body-1">
                {{ chapters.current?.title || 'Chapter' }}
            </VAppBarTitle>
            <VSpacer />
            <VBtn
                icon
                :disabled="!chapters.prev"
                exact
                :to="chapters.prev ? `?ch=${encodeURIComponent(chapters.prev.href)}` : undefined"
            >
                <VIcon>mdi-chevron-left</VIcon>
            </VBtn>
            <span class="text-body-2 mx-2">
                {{ currentChapterIndex + 1 }} / {{ visibleChapters.items.length || 0 }}
            </span>
            <VBtn
                icon
                :disabled="!chapters.next"
                exact
                :to="chapters.next ? `?ch=${encodeURIComponent(chapters.next.href)}` : undefined"
            >
                <VIcon>mdi-chevron-right</VIcon>
            </VBtn>
        </VAppBar>

        <div class="book-reader-content">
            <div v-if="qChapterContent.isLoading.value" class="d-flex justify-center py-8">
                <VProgressCircular indeterminate />
            </div>
            <AQueryError
                v-else-if="qChapterContent.error.value"
                :query="qChapterContent"
                class="ma-4"
            />
            <div v-else ref="chapterContainer" class="book-chapter-container" />

            <div v-if="chapters.next">
                <VDivider />
                <div class="d-flex justify-center pa-4">
                    <VBtn color="primary" :to="`?ch=${encodeURIComponent(chapters.next.href)}`">
                        Next Chapter: {{ chapters.next.title || chapters.next.id }}
                    </VBtn>
                </div>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { renderChapter } from './renderChapterHtml'
import { useVisibleBookChapters } from './useBookDisplayStore'
import AQueryError from '@/components/AQueryError.vue'
import { arrayAtNowrap } from '@/utils/misc'
import { contentApi } from '@/utils/api/content'

const props = defineProps<{
    contentId: string
}>()

const router = useRouter()
const chapterContainer = ref<HTMLDivElement>()

const qChapters = contentApi.useBookChapters(() => props.contentId)
const qChapterContent = contentApi.useBookChapter(
    () => props.contentId,
    () => router.currentRoute.value.query.ch as string
)

const visibleChapters = useVisibleBookChapters(
    computed(() => props.contentId),
    qChapters.data
)

const currentChapterIndex = computed(() => {
    if (!visibleChapters.value) return -1
    return visibleChapters.value.items.findIndex(
        ch => ch.href === router.currentRoute.value.query.ch
    )
})

const chapters = computed(() => {
    const ch = visibleChapters.value.items
    return {
        current: arrayAtNowrap(ch, currentChapterIndex.value),
        prev: arrayAtNowrap(ch, currentChapterIndex.value - 1),
        next: arrayAtNowrap(ch, currentChapterIndex.value + 1),
    }
})

function _renderChapter() {
    if (!chapterContainer.value || !qChapterContent.data.value) return
    renderChapter({
        chapterContainer: chapterContainer.value,
        chapterHtml: qChapterContent.data.value,
        contentId: props.contentId,
        chapterHref: router.currentRoute.value.query.ch as string,
    })
}

watch(() => qChapterContent.data.value, _renderChapter, { immediate: true })
watch(chapterContainer, _renderChapter)
</script>

<style scoped>
.book-reader {
    display: flex;
    flex-direction: column;
    min-height: calc(100dvh - var(--v-layout-top, 0px));
}

.book-reader-toolbar {
    flex-shrink: 0;
}

.book-reader-content {
    flex: 1;
    display: flex;
    flex-direction: column;
}

.book-chapter-container {
    flex: 1;
}
</style>
