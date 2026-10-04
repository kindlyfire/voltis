<template>
    <AAlert v-if="reader.state?.error" tone="danger" class="m-4">
        {{ reader.state.error }}
        <AButton class="mt-2" size="sm" variant="tonal" @click="reader.state.retry()">
            Retry
        </AButton>
    </AAlert>
    <div
        v-else
        class="reader-main select-none"
        @click="controls.handleClick"
        @touchstart.passive="controls.handleTouchStart"
        @touchend.passive="controls.handleTouchEnd"
    >
        <ReaderModePaged v-if="reader.mode === 'paged'" />
        <ReaderModeLongstrip v-else />
    </div>

    <ReaderSidebar />
    <ReaderSaveBanner v-if="reader.sync" :sync="reader.sync" />

    <AProgressBar
        :value="progressValue / 100"
        label="Reading progress"
        class="reader-progress"
        :class="[
            `mode-${reader.mode}`,
            { rtl: reader.mode === 'paged' && reader.direction === 'rtl' },
        ]"
    />
</template>

<script setup lang="ts">
import { useScroll, useWindowSize } from '@vueuse/core'
import { watch, computed, onUnmounted } from 'vue'
import { onBeforeRouteLeave, useRouter } from 'vue-router'
import { useLayoutStore } from '@/pages/_layout/useLayoutStore'
import AAlert from '@/ui/AAlert.vue'
import AButton from '@/ui/AButton.vue'
import AProgressBar from '@/ui/AProgressBar.vue'
import ReaderSaveBanner from '../ReaderSaveBanner.vue'
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

// Not in dispose: unmounting runs after the next page has rendered the old order.
onBeforeRouteLeave(() => {
    reader.leave()
})
onUnmounted(() => {
    reader.dispose()
})

const controls = useReaderControls()

// Not the page count: a zero-page comic would never be ready. Not
// `setHandlers({ onReady })` either: that single slot belongs to the store.
useReaderTutorial(
    'comic',
    computed(() => !!reader.state && !reader.state.loading && !reader.state.error),
    undefined,
    () => reader.controlsFlipped
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

.reader-progress.rtl :deep(.a-progress__fill) {
    transform-origin: right;
}

/* Scrolling drives it continuously; easing would only make it lag. */
.reader-progress.mode-longstrip :deep(.a-progress__fill) {
    transition: none;
}
</style>
