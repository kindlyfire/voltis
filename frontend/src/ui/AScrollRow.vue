<template>
    <section class="a-scroll-row">
        <div class="a-scroll-row__header">
            <h2 :id="titleId" class="a-scroll-row__title">
                <slot name="title">{{ title }}</slot>
            </h2>
            <AIconButton
                :icon="IconChevronLeft"
                :label="`Scroll ${title} back`"
                variant="tonal"
                size="sm"
                :disabled="atStart"
                focusable-when-disabled
                @click="scroll(-1)"
            />
            <AIconButton
                :icon="IconChevronRight"
                :label="`Scroll ${title} forward`"
                variant="tonal"
                size="sm"
                :disabled="atEnd"
                focusable-when-disabled
                @click="scroll(1)"
            />
            <AIconButton
                v-if="seeAll"
                :icon="IconArrowRight"
                :label="`See all ${title}`"
                :to="seeAll"
                variant="tonal"
                size="sm"
            />
        </div>
        <div
            ref="track"
            class="a-scroll-row__track a-focus"
            :class="{ dragging, unsnapped }"
            :style="{ '--item-width': `${itemWidth}px` }"
            role="region"
            :aria-labelledby="titleId"
            tabindex="0"
            @pointerdown="onPointerdown"
            @pointermove="onPointermove"
            @lostpointercapture="endDrag"
            @dragstart.prevent
        >
            <slot />
        </div>
    </section>
</template>

<script setup lang="ts">
import { useDebounceFn, useEventListener, useResizeObserver } from '@vueuse/core'
import { onMounted, onUpdated, ref, useId, useTemplateRef } from 'vue'
import type { RouteLocationRaw } from 'vue-router'
import AIconButton from './AIconButton.vue'
import { IconArrowRight, IconChevronLeft, IconChevronRight } from './icons'

/** A titled, horizontally scrolling row of equal-width items (home page sections). */
withDefaults(
    defineProps<{
        /** Also names the region and the scroll buttons. */
        title: string
        itemWidth?: number
        /** The full listing, linked from the end of the header. */
        seeAll?: RouteLocationRaw
    }>(),
    { itemWidth: 176 }
)

const titleId = useId()
const track = useTemplateRef('track')
const atStart = ref(true)
const atEnd = ref(true)

function measure() {
    const el = track.value
    if (!el) return
    atStart.value = el.scrollLeft <= 1
    atEnd.value = el.scrollLeft + el.clientWidth >= el.scrollWidth - 1
}

onMounted(measure)
useResizeObserver(track, measure)

// Snap is off while the row is pinned to the start, so nothing can re-snap it to the old first
// card when the items reorder. It stays off until the scroll that unpins it ends, so a swipe
// doesn't snap mid-gesture.
const pinned = ref(true)
const unsnapped = ref(true)

function unpin() {
    pinned.value = false
    unsnapped.value = true
}

function onScroll() {
    measure()
    if (pinned.value && !atStart.value) unpin()
}

function settle() {
    // A pending debounce can outlive the row.
    if (drag || !track.value) return
    unsnapped.value = pinned.value = track.value.scrollLeft <= 1
}

useEventListener(track, 'scroll', onScroll, { passive: true })
if ('onscrollend' in window) useEventListener(track, 'scrollend', settle)
else useEventListener(track, 'scroll', useDebounceFn(settle, 150), { passive: true }) // Safari < 26.2 has no scrollend

onUpdated(() => {
    // Undoes scroll anchoring after a reorder. This runs before the scroll event, so onScroll
    // doesn't take it for a user scroll.
    if (pinned.value && !drag) track.value!.scrollTo({ left: 0, behavior: 'instant' })
    measure()
})

// Mouse drag scrolls the row (touch scrolls natively).
const DRAG_THRESHOLD = 5
const dragging = ref(false)
let drag: { pointerId: number; x: number; scrollLeft: number } | null = null

function onPointerdown(e: PointerEvent) {
    if (e.pointerType !== 'mouse' || e.button !== 0 || !track.value) return
    drag = { pointerId: e.pointerId, x: e.clientX, scrollLeft: track.value.scrollLeft }
}

function onPointermove(e: PointerEvent) {
    const el = track.value
    if (!drag || !el || e.pointerId !== drag.pointerId) return
    // The button came up where we didn't see it.
    if (!(e.buttons & 1)) return endDrag()
    const dx = e.clientX - drag.x
    if (!dragging.value) {
        if (Math.abs(dx) < DRAG_THRESHOLD) return
        dragging.value = true
        el.setPointerCapture(e.pointerId)
        getSelection()?.removeAllRanges()
    }
    el.scrollLeft = drag.scrollLeft - dx
}

function endDrag() {
    const active = drag !== null
    drag = null
    dragging.value = false
    // Not on a touch pan's pointercancel, which mustn't snap a row still flinging. A release may
    // not scroll, so scrollend alone won't settle it.
    if (active) settle()
}

// On window: the button may come up outside the row before the drag starts capturing.
useEventListener(window, 'pointercancel', endDrag)
useEventListener(window, 'pointerup', (e: PointerEvent) => {
    if (!drag || e.pointerId !== drag.pointerId) return
    const dragged = dragging.value
    endDrag()
    if (!dragged) return
    // The click ending a drag must not open the card under the pointer. It follows pointerup in
    // the same task, if at all.
    const suppress = (ev: MouseEvent) => {
        ev.preventDefault()
        ev.stopPropagation()
    }
    window.addEventListener('click', suppress, { capture: true, once: true })
    setTimeout(() => window.removeEventListener('click', suppress, { capture: true }))
})

function scroll(direction: 1 | -1) {
    const el = track.value
    if (!el) return
    // An item change during the smooth scroll must not reset it.
    if (direction === 1) unpin()
    el.scrollBy({ left: el.clientWidth * 0.8 * direction })
}
</script>

<style scoped>
@layer ui {
    .a-scroll-row {
        display: flex;
        flex-direction: column;
        gap: 14px;
        min-width: 0;
    }

    .a-scroll-row__header {
        display: flex;
        align-items: center;
        gap: 6px;
    }

    /* The design's row buttons: 36px on s2. */
    .a-scroll-row__header :deep(.a-icon-button) {
        --btn-size: 36px;
        --icon-size: 22px;
        --btn-bg: var(--color-surface-2);
        --btn-fg: var(--color-fg-muted);
    }

    .a-scroll-row__title {
        min-width: 0;
        margin-inline-end: 12px;
        font-family: var(--font-display);
        font-size: 30px;
        font-weight: 600;
        line-height: 1.2;
    }

    .a-scroll-row__track {
        display: grid;
        grid-auto-columns: var(--item-width);
        grid-auto-flow: column;
        gap: 18px;
        overflow-x: auto;
        /* Room for the cards' focus rings and shadows, which the scroll container would clip;
           keeps the design's 4px top / 8px bottom offsets. */
        margin: -2px -6px 0;
        padding: 6px 6px 8px;
        border-radius: 12px;
        scroll-behavior: smooth;
        scroll-padding-inline: 6px;
        scroll-snap-type: x proximity;
        scrollbar-width: none;

        &::-webkit-scrollbar {
            display: none;
        }

        & > :deep(*) {
            scroll-snap-align: start;
        }

        @media (pointer: fine) {
            cursor: grab;
        }

        &.unsnapped {
            scroll-snap-type: none;
        }

        /* Snapping resumes on release, which settles the row on a card. */
        &.dragging {
            cursor: grabbing;
            scroll-behavior: auto;
            scroll-snap-type: none;
            user-select: none;

            & :deep(*) {
                cursor: inherit;
            }
        }
    }

    @media (width < 40rem) {
        .a-scroll-row__title {
            font-size: 24px;
        }

        .a-scroll-row__track {
            grid-auto-columns: calc(var(--item-width) * 0.75);
            gap: 14px;
        }
    }
}
</style>
