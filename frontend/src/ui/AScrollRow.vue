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
        </div>
        <div
            ref="track"
            class="a-scroll-row__track a-focus"
            :style="{ '--item-width': `${itemWidth}px` }"
            role="region"
            :aria-labelledby="titleId"
            tabindex="0"
            @scroll.passive="measure"
        >
            <slot />
        </div>
    </section>
</template>

<script setup lang="ts">
import { useMutationObserver, useResizeObserver } from '@vueuse/core'
import { onMounted, ref, useId, useTemplateRef } from 'vue'
import AIconButton from './AIconButton.vue'
import { IconChevronLeft, IconChevronRight } from './icons'

/** A titled, horizontally scrolling row of equal-width items (home page sections). */
withDefaults(
    defineProps<{
        /** Also names the region and the scroll buttons. */
        title: string
        itemWidth?: number
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
useMutationObserver(track, measure, { childList: true, subtree: true })

function scroll(direction: 1 | -1) {
    const el = track.value
    if (!el) return
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
