<template>
    <div class="reader-main select-none" @click="controls.handleClick">
        <ReaderModePaged v-if="reader.mode === 'paged'" />
        <ReaderModeLongstrip v-else />
    </div>

    <ReaderSidebar />

    <AProgressBar
        :value="progressValue / 100"
        label="Reading progress"
        class="reader-progress"
        :class="`mode-${reader.mode}`"
    />
</template>

<script setup lang="ts">
import { useScroll, useWindowSize } from '@vueuse/core'
import { watch, computed, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useLayoutStore } from '@/pages/_layout/useLayoutStore'
import AProgressBar from '@/ui/AProgressBar.vue'
import { useReaderTutorial } from '../useReaderTutorial'
import ReaderModeLongstrip from './ReaderModeLongstrip.vue'
import ReaderModePaged from './ReaderModePaged.vue'
import ReaderSidebar from './ReaderSidebar.vue'
import { useReaderStore } from './useComicDisplayStore'
import { useReaderControls } from './useReaderControls'

const props = defineProps<{
    contentId: string
}>()

const router = useRouter()
const reader = useReaderStore()
const layout = useLayoutStore()
layout.sidebarTemporary.useLayer('comicReader', true)

// Set content when props change
watch(
    () => props.contentId,
    () => {
        const _page = router.currentRoute.value.query.page
        const _pageN = parseInt(_page as string)
        const initialPage = ['last', 'resume'].includes(_page as string)
            ? (_page as 'last' | 'resume')
            : isNaN(_pageN)
              ? 0
              : _pageN - 1
        reader.setContent({
            contentId: props.contentId,
            initialPage,
        })
    },
    { immediate: true }
)

onUnmounted(() => {
    reader.dispose()
})

const controls = useReaderControls()

// Not the page count: a zero-page comic would never be ready. Not
// `setHandlers({ onReady })` either: that single slot belongs to the store.
useReaderTutorial(
    'comic',
    computed(() => !!reader.state && !reader.state.loading && !reader.state.error)
)

const { y: scrollY } = useScroll(window)
const { height: windowHeight } = useWindowSize()
const progressValue = computed(() => {
    if (reader.mode === 'paged') return reader.progress
    scrollY.value
    const maxScroll = document.documentElement.scrollHeight - windowHeight.value
    if (maxScroll <= 0) return 0
    return Math.min(100, (scrollY.value / maxScroll) * 100)
})
</script>

<style scoped>
.reader-main {
    position: relative;
    width: 100%;
    min-height: calc(100dvh - var(--layout-top, 0px));
}

.reader-progress {
    position: fixed;
    bottom: 0;
    left: 0;
    right: 0;
    z-index: var(--z-reader-progress);
    border-radius: 0;
    pointer-events: none;
}

/* Scrolling drives it continuously; easing would only make it lag. */
.reader-progress.mode-longstrip :deep(.a-progress__fill) {
    transition: none;
}
</style>
