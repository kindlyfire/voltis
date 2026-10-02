<template>
    <div
        ref="containerRef"
        id="longstrip-container"
        class="reader-longstrip flex flex-col items-center"
    >
        <template v-for="(loader, index) in reader.state?.loaders ?? []" :key="index">
            <div
                class="reader-longstrip__page"
                :id="`longstrip-page-${index}`"
                :style="getPageStyle(index)"
            >
                <div
                    v-if="loader.error"
                    class="reader-longstrip__placeholder flex flex-col items-center justify-center gap-2"
                >
                    <div class="text-(--color-error)">{{ loader.error }}</div>
                    <AButton variant="tonal" size="sm" @click.stop="loader.load()">Retry</AButton>
                </div>
                <div
                    v-else-if="loader.loading || !loader.blobUrl"
                    class="reader-longstrip__placeholder flex items-center justify-center"
                >
                    <ASpinner decorative />
                </div>
                <img
                    v-else
                    :src="loader.blobUrl"
                    :alt="`Page ${index + 1}`"
                    class="reader-longstrip__image"
                />
            </div>
        </template>
    </div>

    <ComicEndCard v-if="reader.state && !reader.state.loading && !reader.siblings.next" />
    <div class="browser-ui-padding"></div>
</template>

<script setup lang="ts">
import { useEventListener } from '@vueuse/core'
import { ref, onMounted } from 'vue'
import { useNavbarScrollHide } from '@/pages/_layout/useLayoutStore'
import AButton from '@/ui/AButton.vue'
import ASpinner from '@/ui/ASpinner.vue'
import { keysOwnedElsewhere } from '@/ui/overlay'
import ComicEndCard from './ComicEndCard.vue'
import { pageStyle, useReaderStore } from './useComicDisplayStore'

const reader = useReaderStore()
const containerRef = ref<HTMLElement | null>(null)
useNavbarScrollHide()

function getPageStyle(index: number) {
    const page = reader.state?.pageDimensions[index]
    return page ? pageStyle(page, reader.settings.longstripWidth) : {}
}

/** The page at the viewport's centre, taken at each scroll: reading, unless a placement or a
 * reflow moved the strip. */
function updateCurrentPage() {
    const children = containerRef.value?.children
    if (!children?.length) return
    const centre = window.scrollY + window.innerHeight / 2
    let lo = 0
    let hi = children.length - 1
    while (lo < hi) {
        const mid = Math.ceil((lo + hi) / 2)
        if ((children[mid] as HTMLElement).offsetTop <= centre) lo = mid
        else hi = mid - 1
    }
    reader.setPage(lo, { restore: reader.restoring || !reader.armed })
}

const SCROLL_KEYS = new Set(['ArrowDown', 'ArrowUp', 'PageDown', 'PageUp', ' ', 'Home', 'End'])

/** Input on the strip itself (or its scrollbar), not on the sidebar, a drawer or a dialog. */
function arm(e: Event) {
    const target = e.target
    if (e instanceof KeyboardEvent) {
        if (!SCROLL_KEYS.has(e.key) || reader.sidebarOpen || keysOwnedElsewhere(e)) return
    } else if (
        target !== document.documentElement &&
        !(target instanceof Node && document.querySelector('.reader-main')?.contains(target))
    ) {
        return
    }
    reader.armed = true
}

let lastScrollY = window.scrollY
/** Scrolled down by the reader until the last page's bottom is in view: a deliberate finish. */
function checkFinish() {
    const down = window.scrollY > lastScrollY
    lastScrollY = window.scrollY
    if (!down || !reader.armed || reader.restoring || !containerRef.value) return
    const last = containerRef.value.lastElementChild
    if (!last || last.getBoundingClientRect().bottom > window.innerHeight) return
    reader.state?.finish()
}

useEventListener(window, 'scroll', () => {
    updateCurrentPage()
    checkFinish()
})
useEventListener(window, ['wheel', 'touchstart', 'pointerdown', 'keydown'], arm, { passive: true })
// A reflow moves the strip under the reader: not reading until the next input.
useEventListener(window, ['resize', 'orientationchange'], () => reader.placement())

onMounted(() => {
    reader.goToPage()
})
</script>

<style scoped>
.reader-longstrip {
    width: 100%;
    min-height: calc(100dvh - var(--layout-top, 0px));
}

.reader-longstrip__page {
    max-width: 100%;
}

.reader-longstrip__placeholder {
    width: 100%;
    height: 100%;
    min-height: 200px;
    background: rgba(128, 128, 128, 0.1);
}

.reader-longstrip__image {
    width: 100%;
    height: auto;
    display: block;
}

.browser-ui-padding {
    height: calc(100lvh - 100svh);
}
</style>
