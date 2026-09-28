<template>
    <div
        class="book-reader flex flex-col"
        :class="{ 'is-paged': paged }"
        :style="readerVars"
        @pointerdown="controls.handlePointerDown"
        @mousedown="controls.handleMouseDown"
        @click="controls.handleClick"
        @wheel.passive="controls.handleWheel"
        @touchstart.passive="controls.handleTouchStart"
        @touchend.passive="controls.handleTouchEnd"
    >
        <AAlert
            v-if="session?.notice"
            dismissible
            class="book-notice m-4"
            @click.stop
            @dismiss="session.dismissNotice()"
        >
            {{ session.notice }}
        </AAlert>

        <div v-if="!session || session.loading" class="flex justify-center py-8">
            <ASpinner />
        </div>
        <AAlert v-else-if="session.error" tone="danger" class="m-4">
            {{ session.error }}
        </AAlert>

        <template v-if="session">
            <template v-if="ready && !paged">
                <div v-if="session.standalone" class="flex justify-center p-4">
                    <AButton
                        variant="tonal"
                        :leading-icon="IconArrowLeft"
                        @click.stop="leaveStandalone"
                    >
                        Back to reading
                    </AButton>
                </div>
                <div v-else-if="session.prevChapter" class="flex flex-col items-center gap-1 p-4">
                    <AButton
                        variant="tonal"
                        :aria-describedby="prevTitleId"
                        @click.stop="goChapter($event, -1)"
                    >
                        Previous chapter
                    </AButton>
                    <span :id="prevTitleId" class="text-fg-muted text-center text-sm wrap-anywhere">
                        {{ session.prevChapter.title }}
                    </span>
                </div>
            </template>

            <!-- Keyed: preserved through this book's loading and errors,
            replaced when the book itself changes. Both layouts share the host,
            so switching never remounts the slices. -->
            <div :key="session.contentId" class="book-viewport relative flex-1">
                <div ref="host" class="book-host" :inert="paged && session.atBookEnd" />
                <BookEndScreen
                    v-if="paged && session.atBookEnd"
                    :content-id="contentId"
                    :next="nextVolume"
                    @open="openVolume"
                    @leave="session.snapshotPassage()"
                    @blur="nextPageButton?.focus()"
                />
            </div>

            <template v-if="ready && !session.standalone && !paged">
                <ADivider />
                <div class="flex flex-col items-center gap-1 p-4">
                    <span v-if="!session.nextChapter" class="text-fg-muted mb-2 text-sm">
                        End of book
                    </span>
                    <template v-if="next">
                        <AButton :aria-describedby="nextTitleId" @click.stop="next.go">
                            {{ next.label }}
                        </AButton>
                        <span
                            :id="nextTitleId"
                            class="text-fg-muted text-center text-sm wrap-anywhere"
                        >
                            {{ next.title }}
                        </span>
                    </template>
                </div>
                <div ref="sentinel" class="h-px" />
            </template>

            <div
                v-if="paged"
                class="book-footer text-fg-muted flex h-10 shrink-0 items-center justify-center gap-4 text-sm"
            >
                <button
                    type="button"
                    class="page-button sr-only focus-visible:not-sr-only"
                    @click.stop="turnPage($event, 'prev')"
                >
                    Previous page
                </button>
                <AButton
                    v-if="ready && session.standalone"
                    variant="text"
                    size="sm"
                    :leading-icon="IconArrowLeft"
                    @click.stop="leaveStandalone"
                >
                    Back to reading
                </AButton>
                <span v-else-if="counter" aria-live="polite">
                    Page {{ counter.index + 1 }} / {{ counter.count }}
                </span>
                <button
                    ref="nextPageButton"
                    type="button"
                    class="page-button sr-only focus-visible:not-sr-only"
                    @click.stop="turnPage($event, 'next')"
                >
                    Next page
                </button>
            </div>
        </template>
    </div>

    <BookReaderDrawer :content-id="contentId" />

    <AProgressBar
        v-if="session?.layoutMode === 'scroll' && session.firstChapterMounted"
        :value="chapterProgress"
        label="Chapter progress"
        class="reader-progress"
    />
</template>

<script setup lang="ts">
import { useStyleTag } from '@vueuse/core'
import { computed, onUnmounted, ref, useId, watch } from 'vue'
import { useRouter } from 'vue-router'
import AAlert from '@/ui/AAlert.vue'
import AButton from '@/ui/AButton.vue'
import ADivider from '@/ui/ADivider.vue'
import AProgressBar from '@/ui/AProgressBar.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { IconArrowLeft } from '@/ui/icons'
import { useReaderTutorial } from '../useReaderTutorial'
import BookEndScreen from './BookEndScreen.vue'
import BookReaderDrawer from './BookReaderDrawer.vue'
import { FONT_STACKS, type BookFont } from './bookSettings'
import { PAGED_CSS } from './pagedCss'
import { useBookControls } from './useBookControls'
import { useBookDisplayStore } from './useBookDisplayStore'
import { useChapterScrollProgress } from './useChapterScrollProgress'
import { useNextVolume } from './useNextVolume'

defineProps<{ contentId: string }>()

const store = useBookDisplayStore()
const session = computed(() => store.session)
const ready = computed(() => !!session.value && !session.value.loading && !session.value.error)
const paged = computed(() => session.value?.layoutMode === 'paged')
const host = ref<HTMLDivElement>()
const sentinel = ref<HTMLDivElement>()
const nextPageButton = ref<HTMLButtonElement>()
const prevTitleId = useId()
const nextTitleId = useId()
const controls = useBookControls()
const router = useRouter()
const chapterProgress = useChapterScrollProgress(host)
const nextVolume = useNextVolume(
    () => session.value?.content ?? null,
    () => ready.value && !!session.value?.chapters.length && !session.value.nextChapter
)

// Global: the paged rules reach the `<html>` element and the slice hosts.
useStyleTag(PAGED_CSS, { id: 'book-paged-css' })

const counter = computed(() => {
    const current = session.value
    if (!ready.value || !current?.firstChapterMounted || current.atBookEnd) return null
    return current.screen
})

useReaderTutorial(
    'book',
    computed(() => ready.value && !!session.value?.firstChapterMounted),
    () => session.value?.layoutMode
)

const publisherFonts = computed(() => store.settings.fontFamily === 'publisher')

/** Custom properties inherit into the per-slice shadow roots. */
const readerVars = computed(() => ({
    '--reader-font-size': `${store.settings.fontSize}rem`,
    '--reader-font-family': publisherFonts.value
        ? undefined
        : FONT_STACKS[store.settings.fontFamily as BookFont],
    '--reader-line-height': `${store.settings.lineHeight}`,
    '--reader-max-width': `calc(${store.settings.width}em + 4rem)`,
    '--reader-width': `${store.settings.width}`,
}))

const next = computed(() => {
    const chapter = session.value?.nextChapter
    if (chapter) {
        return {
            label: 'Next chapter',
            title: chapter.title,
            go: (event: MouseEvent) => goChapter(event, 1),
        }
    }
    const volume = nextVolume.value
    return volume && { label: 'Next volume', title: volume.title, go: () => openVolume(volume.id) }
})

/** Blurred: the button survives the route change with focus, where the next
 * Space would re-activate it instead of scrolling. */
function goChapter(event: MouseEvent, delta: number) {
    ;(event.currentTarget as HTMLElement).blur()
    const current = session.value
    current?.goToChapter(current.chapterIndex + delta)
}

/** Blurred for the same reason, unless it was reached by keyboard. */
function turnPage(event: MouseEvent, direction: 'next' | 'prev') {
    if (event.detail) (event.currentTarget as HTMLElement).blur()
    session.value?.turn(direction)
}

/** The next session resumes from that book's own saved position; disposing
 * this one flushes its completion. */
function openVolume(id: string) {
    void router.push({ name: 'read-content', params: { id } })
}

function leaveStandalone(event: MouseEvent) {
    ;(event.currentTarget as HTMLElement).blur()
    void session.value?.closeStandalone()
}

watch(
    [session, host, sentinel],
    () => {
        session.value?.setElements({
            host: host.value ?? null,
            sentinel: sentinel.value ?? null,
        })
    },
    { immediate: true, flush: 'post' }
)

// No pull-to-refresh or horizontal back swipe over the pages.
watch(
    paged,
    value => {
        document.documentElement.classList.toggle('book-paged', value)
    },
    { immediate: true }
)

onUnmounted(() => {
    document.documentElement.classList.remove('book-paged')
    session.value?.setElements({ host: null, sentinel: null })
})
</script>

<style scoped>
.book-reader {
    min-height: calc(100dvh - var(--layout-top, 0px));
}

/* Nothing may widen the page: on Android Firefox the layout viewport grows with it, pushing
 * right-anchored fixed elements off screen. `clip` isn't a scroll container, so sticky and
 * scroll anchoring still work. */
.book-reader:not(.is-paged) {
    overflow-x: clip;
}

/* Each mounted slice's shadow host (see `mountTree`). */
.book-reader :deep(.book-slice) {
    display: block;
    box-sizing: border-box;
    max-width: var(--reader-max-width, calc(45em + 4rem));
    margin: 0 auto;
    padding: 2rem;
}

/* Over the page, never shrinking it. */
.book-reader.is-paged .book-notice {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    z-index: 1;
}

.page-button:focus-visible {
    padding: 0.25rem 0.5rem;
    border-radius: 0.25rem;
    outline: 2px solid var(--color-primary);
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
.reader-progress :deep(.a-progress__fill) {
    transition: none;
}
</style>
