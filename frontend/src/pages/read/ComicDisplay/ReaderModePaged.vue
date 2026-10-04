<template>
    <div class="reader-paged relative">
        <div
            ref="viewport"
            class="reader-paged__viewport transition-none"
            tabindex="-1"
            @scroll.passive="recordScroll"
            @scrollend="pending = null"
            @wheel.passive="pending = null"
            @touchmove.passive="pending = null"
        >
            <div
                class="reader-paged__spread transition-none"
                :class="{ 'flex-row-reverse': reader.direction === 'rtl', 'size-full': !boxes }"
                :style="boxes ? px(boxes.width, boxes.height) : undefined"
                :role="slots.length === 2 ? 'figure' : undefined"
                :aria-label="
                    slots.length === 2
                        ? `Pages ${slots[0]!.page + 1}–${slots[1]!.page + 1}`
                        : undefined
                "
            >
                <div
                    v-for="({ page, loader }, i) in slots"
                    :key="page"
                    class="flex items-center justify-center"
                    :class="boxes ? 'shrink-0 transition-none' : 'h-full min-w-0 flex-1'"
                    :style="boxes ? px(boxes.pages[i]!.width, boxes.pages[i]!.height) : undefined"
                >
                    <div v-if="loader?.error" class="flex flex-col items-center gap-2">
                        <div class="text-(--color-error)">{{ loader.error }}</div>
                        <AButton variant="tonal" @click.stop="loader.load()">Retry</AButton>
                    </div>
                    <ASpinner v-else-if="!loader?.blobUrl || loader.loading" size="lg" />
                    <img
                        v-else
                        :src="loader.blobUrl"
                        :alt="`Page ${page + 1}`"
                        class="size-full object-contain"
                        :style="!boxes && slots.length === 2 && { objectPosition: meetSide(i) }"
                    />
                </div>
            </div>
        </div>
        <ComicEndCard
            v-if="reader.atEnd"
            class="reader-paged__end absolute inset-0 justify-center"
        />
    </div>
</template>

<script setup lang="ts">
import { useElementSize, useMediaQuery } from '@vueuse/core'
import { computed, onMounted, onUnmounted, useTemplateRef, watch } from 'vue'
import { useLayoutStore } from '@/pages/_layout/useLayoutStore'
import AButton from '@/ui/AButton.vue'
import ASpinner from '@/ui/ASpinner.vue'
import ComicEndCard from './ComicEndCard.vue'
import { layoutSpread, scrollStep } from './pagedLayout'
import type { PagedScroller } from './types'
import { useReaderStore } from './useComicDisplayStore'

const reader = useReaderStore()
const layout = useLayoutStore()
layout.navbarHidden.useLayer('comicReaderPaged', true)

const viewport = useTemplateRef('viewport')
const size = useElementSize(viewport, { width: window.innerWidth, height: window.innerHeight })
const reducedMotion = useMediaQuery('(prefers-reduced-motion: reduce)')

const spread = computed(() => reader.currentSpread ?? [])
const slots = computed(() =>
    spread.value.map(page => ({ page, loader: reader.state?.loaders[page] }))
)
/** Whole px, so rounding never makes a fitting spread overflow. Null when a size is unknown. */
const boxes = computed(() => {
    const dims = reader.state?.pageDimensions
    if (!dims || !spread.value.length) return null
    const l = layoutSpread(
        spread.value.map(p => dims[p]!),
        { width: size.width.value, height: size.height.value },
        { fit: reader.settings.fit, zoomWide: reader.settings.zoomWide }
    )
    if (!l) return null
    const height = Math.floor(l.height)
    const pages = l.pages.map(p => ({ width: Math.floor(p.width), height }))
    return { pages, width: pages.reduce((sum, p) => sum + p.width, 0), height }
})
const px = (width: number, height: number) => ({ width: `${width}px`, height: `${height}px` })
// Unsized pages meet at the spine.
const meetSide = (i: number) => ((i === 0) !== (reader.direction === 'rtl') ? 'right' : 'left')

type Axis = 'x' | 'y' | null
function overflowAxis(el: HTMLElement): Axis {
    if (el.scrollWidth > el.clientWidth + 1) return 'x'
    if (el.scrollHeight > el.clientHeight + 1) return 'y'
    return null
}
const rtl = () => reader.direction === 'rtl'
const maxOf = (el: HTMLElement, axis: 'x' | 'y') =>
    axis === 'x' ? el.scrollWidth - el.clientWidth : el.scrollHeight - el.clientHeight
/** Distance from the reading-order start along an axis; mirrored on x in RTL. */
function readPos(el: HTMLElement, axis: 'x' | 'y') {
    if (axis === 'y') return el.scrollTop
    return rtl() ? maxOf(el, 'x') - el.scrollLeft : el.scrollLeft
}
function scrollOptions(el: HTMLElement, axis: 'x' | 'y', pos: number): ScrollToOptions {
    if (axis === 'y') return { top: pos }
    return { left: rtl() ? maxOf(el, 'x') - pos : pos }
}
const smooth = () => !reducedMotion.value && 'onscrollend' in window

// The target of the step under way, so a quick second step continues from where the first ends.
let pending: ScrollToOptions | null = null

/** Places the reading position at a fraction of the overflow, cancelling any smooth scroll. */
function place(el: HTMLElement, fraction: number) {
    pending = null
    const axis = overflowAxis(el)
    const target = axis && scrollOptions(el, axis, fraction * maxOf(el, axis))
    el.scrollTo({ left: 0, top: 0, ...target, behavior: 'instant' })
}

const scroller: PagedScroller = {
    enterAt: null,
    step(dir) {
        const el = viewport.value
        const axis = el && overflowAxis(el)
        if (!axis) return false
        if (pending) el.scrollTo({ ...pending, behavior: 'instant' })
        pending = null
        const target = scrollStep(
            readPos(el, axis),
            maxOf(el, axis),
            axis === 'x' ? el.clientWidth : el.clientHeight,
            dir === 'next'
        )
        if (target === null) return false
        const options = scrollOptions(el, axis, target)
        const animate = smooth()
        if (animate) pending = options
        el.scrollTo({ ...options, behavior: animate ? 'smooth' : 'instant' })
        return true
    },
    nudge(fraction) {
        const el = viewport.value
        if (!el) return
        pending = null
        el.scrollBy({
            top: fraction * el.clientHeight,
            behavior: smooth() ? 'smooth' : 'instant',
        })
    },
    edges() {
        const el = viewport.value
        if (!el || overflowAxis(el) !== 'x') return { left: true, right: true }
        return { left: el.scrollLeft <= 2, right: el.scrollLeft >= maxOf(el, 'x') - 2 }
    },
}
onMounted(() => (reader.pagedScroller = scroller))
onUnmounted(() => {
    if (reader.pagedScroller === scroller) reader.pagedScroller = null
})

// The reading fraction before a resize, read back once the new layout is in place.
let lastAxis: Axis = null
let lastFraction = 0
function recordScroll() {
    const el = viewport.value
    if (!el) return
    lastAxis = overflowAxis(el)
    lastFraction = lastAxis ? readPos(el, lastAxis) / maxOf(el, lastAxis) : 0
}

watch(
    [
        () => viewport.value,
        () => spread.value.join(','),
        () => reader.direction,
        () => boxes.value?.width,
        () => boxes.value?.height,
        () => size.width.value,
        () => size.height.value,
    ],
    ([el, key, direction], [oldEl, oldKey, oldDirection]) => {
        if (!el) return
        if (el !== oldEl || key !== oldKey) {
            const enterAt = scroller.enterAt
            scroller.enterAt = null
            place(el, enterAt === 'end' ? 1 : 0)
            // A turn, not a placement: slide in from the side being read toward.
            if (enterAt && !reducedMotion.value) {
                const offset = (enterAt === 'start') === !rtl() ? 24 : -24
                el.animate(
                    [
                        { transform: `translateX(${offset}px)`, opacity: 0.6 },
                        { transform: 'none', opacity: 1 },
                    ],
                    { duration: 160, easing: 'ease-out' }
                )
            }
        } else if (direction !== oldDirection) {
            place(el, 0)
        } else {
            // Narrowing the viewport alone can make an unchanged spread overflow.
            place(el, overflowAxis(el) === lastAxis ? lastFraction : 0)
        }
        recordScroll()
    },
    { flush: 'post' }
)
</script>

<style scoped>
.reader-paged {
    width: 100%;
    height: 100dvh;
    /* The page-turn slide offsets the viewport. */
    overflow: hidden;
}

.reader-paged__viewport {
    display: flex;
    width: 100%;
    height: 100dvh;
    overflow: auto;
    overscroll-behavior: contain;
    /* Classic scrollbars would shrink the box the layout is sized from. */
    scrollbar-width: none;

    &::-webkit-scrollbar {
        display: none;
    }
}

.reader-paged__spread {
    display: flex;
    flex: none;
    /* Centres a fitting spread; unlike justify-content, keeps an overflowing one scrollable from
    its start edge. */
    margin: auto;
}

.reader-paged__end {
    background: var(--color-bg);
}
</style>
