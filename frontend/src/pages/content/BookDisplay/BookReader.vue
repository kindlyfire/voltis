<template>
    <div class="book-reader">
        <VAppBar density="compact" class="book-reader-toolbar">
            <VBtn icon :to="{ query: {} }" variant="text" exact>
                <VIcon>mdi-arrow-left</VIcon>
            </VBtn>
            <VAppBarTitle class="text-body-1">
                {{ currentChapter?.title || 'Chapter' }}
            </VAppBarTitle>
            <VSpacer />
            <VBtn icon :disabled="!prevChapter" @click="goToChapter(prevChapter?.href)">
                <VIcon>mdi-chevron-left</VIcon>
            </VBtn>
            <span class="text-body-2 mx-2">
                {{ currentChapterIndex + 1 }} / {{ store.visibleChapters?.length || 0 }}
            </span>
            <VBtn icon :disabled="!nextChapter" @click="goToChapter(nextChapter?.href)">
                <VIcon>mdi-chevron-right</VIcon>
            </VBtn>
        </VAppBar>

        <div class="book-reader-content">
            <div v-if="store.qChapterContent.isLoading" class="d-flex justify-center py-8">
                <VProgressCircular indeterminate />
            </div>
            <AQueryError
                v-else-if="store.qChapterContent.error"
                :query="store.qChapterContent"
                class="ma-4"
            />
            <div v-else ref="chapterContainer" class="book-chapter-container" />

            <div v-if="nextChapter">
                <VDivider />
                <div class="d-flex justify-center pa-4">
                    <VBtn color="primary" @click="goToChapter(nextChapter?.href)">
                        Next Chapter: {{ nextChapter?.title || nextChapter?.id }}
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
import { useBookDisplayStore } from './useBookDisplayStore'
import AQueryError from '@/components/AQueryError.vue'

const router = useRouter()
const chapterContainer = ref<HTMLDivElement>()
const store = useBookDisplayStore()

const currentChapterIndex = computed(() => {
    if (!store.visibleChapters) return -1
    return store.visibleChapters.findIndex(ch => ch.href === store.chapterHref)
})

const currentChapter = computed(() => {
    if (currentChapterIndex.value < 0) return null
    return store.visibleChapters?.[currentChapterIndex.value] ?? null
})

const prevChapter = computed(() => {
    if (currentChapterIndex.value <= 0) return null
    return store.visibleChapters?.[currentChapterIndex.value - 1] ?? null
})

const nextChapter = computed(() => {
    if (!store.visibleChapters) return null
    if (currentChapterIndex.value >= store.visibleChapters.length - 1) return null
    return store.visibleChapters[currentChapterIndex.value + 1] ?? null
})

function goToChapter(href: string | undefined) {
    if (href) {
        router.push({ query: { ch: href } })
    }
}

function _renderChapter() {
    if (!chapterContainer.value || !store.chapterContent) return
    renderChapter({
        chapterContainer: chapterContainer.value,
        chapterHtml: store.chapterContent,
        contentId: store.contentId!,
        chapterHref: store.chapterHref!,
    })
}

watch(() => store.chapterContent, _renderChapter, { immediate: true })
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
