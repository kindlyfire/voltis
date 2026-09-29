<template>
    <div
        ref="track"
        class="a-scrubber"
        :class="{ handle: handleMode, shown: dragging || recent }"
        :style="{ '--pos': thumbPos, '--pointer': pointer ?? 0 }"
        role="slider"
        tabindex="0"
        aria-orientation="vertical"
        :aria-label="label"
        aria-valuemin="0"
        :aria-valuemax="segments.length - 1"
        :aria-valuenow="current"
        :aria-valuetext="segments[current]?.bubble"
        data-no-drawer-swipe
        @pointerdown="onPointerDown"
        @pointermove="onPointerMove"
        @pointerup="onPointerEnd"
        @pointercancel="onPointerEnd"
        @lostpointercapture="onPointerEnd"
        @pointerleave="onPointerLeave"
        @keydown="onKeydown"
    >
        <template v-if="!handleMode">
            <span
                v-for="(mark, i) in marks"
                :key="i"
                class="a-scrubber__mark"
                :style="{ top: `${mark.y}px` }"
                aria-hidden="true"
            >
                <template v-if="mark.label">{{ mark.label }}</template>
                <span v-else class="a-scrubber__dot" />
            </span>
            <span class="a-scrubber__thumb" />
        </template>
        <span v-else ref="handle" class="a-scrubber__handle">
            <AIcon :icon="IconChevronUp" :size="16" />
            <AIcon :icon="IconChevronDown" :size="16" />
        </span>
        <span v-if="pointer !== null" class="a-scrubber__bubble" aria-hidden="true">
            {{ segments[segmentAt(pointer)]?.bubble }}
        </span>
    </div>
</template>

<script setup lang="ts">
import { useElementSize, useEventListener, useMediaQuery, useTimeoutFn } from '@vueuse/core'
import { computed, onScopeDispose, ref, useTemplateRef, watch } from 'vue'
import AIcon from './AIcon.vue'
import { IconChevronDown, IconChevronUp } from './icons'

const props = defineProps<{
    /** In list order; `start` is where the segment begins on the rail, 0–1. */
    segments: { label: string; bubble: string; start: number }[]
    /** The scroll position, 0–1. */
    modelValue: number
    /** Accessible name, e.g. "Jump to". */
    label: string
    /** How far `modelValue` may land from a keyboard seek's target and still count as there. */
    tolerance: number
}>()

const emit = defineEmits<{ seek: [position: number]; dragstart: []; dragend: [] }>()

const LABEL_GAP = 16
const DOT_GAP = 8
const HANDLE_HEIGHT = 44

const coarse = useMediaQuery('(pointer: coarse)')
// Below `nav` the frame's 16px padding is too narrow for labels.
const narrow = useMediaQuery('(width < 60rem)')
const handleMode = computed(() => coarse.value || narrow.value)

const track = useTemplateRef('track')
const handle = useTemplateRef('handle')
const { height } = useElementSize(track)

// A segment gets its label 16px below the last label, else a dot 8px below the last mark, else
// nothing. A label drops a dot less than 8px above it.
const marks = computed(() => {
    const out: { y: number; label: string | null }[] = []
    let lastLabel = -Infinity
    for (const s of props.segments) {
        const y = Math.min(s.start * height.value, height.value - LABEL_GAP)
        const prev = out.at(-1)
        if (y - lastLabel >= LABEL_GAP) {
            if (prev && !prev.label && y - prev.y < DOT_GAP) out.pop()
            out.push({ y, label: s.label })
            lastLabel = y
        } else if (!prev || y - prev.y >= DOT_GAP) {
            out.push({ y, label: null })
        }
    }
    return out
})

function segmentAt(position: number) {
    return Math.max(
        0,
        props.segments.findLastIndex(s => s.start <= position)
    )
}

// Pointer: the 0–1 position under a hovering or dragging pointer.
const pointer = ref<number | null>(null)
const dragging = ref(false)
const thumbPos = computed(() => (dragging.value ? pointer.value! : props.modelValue))
let dragId = 0
let grab = 0
let lastY = 0
let frame = 0
onScopeDispose(() => cancelAnimationFrame(frame))

function positionAt(clientY: number) {
    const rect = track.value!.getBoundingClientRect()
    const range = rect.height - (handleMode.value ? HANDLE_HEIGHT : 0)
    return Math.min(1, Math.max(0, (clientY - grab - rect.top) / range))
}

function update() {
    frame = 0
    pointer.value = positionAt(lastY)
    if (dragging.value) emit('seek', pointer.value)
}

function onPointerDown(e: PointerEvent) {
    if (e.button !== 0 || dragging.value) return
    track.value!.setPointerCapture(e.pointerId)
    dragId = e.pointerId
    // A handle moves by the pointer's travel, not to the pointer.
    grab = handle.value ? e.clientY - handle.value.getBoundingClientRect().top : 0
    lastY = e.clientY
    dragging.value = true
    emit('dragstart')
    cancelAnimationFrame(frame)
    update()
}

function onPointerMove(e: PointerEvent) {
    if (dragging.value ? e.pointerId !== dragId : e.pointerType === 'touch') return
    lastY = e.clientY
    frame ||= requestAnimationFrame(update)
}

// Up, cancel and lost capture all end the drag; the first one wins.
function onPointerEnd(e: PointerEvent) {
    if (!dragging.value || e.pointerId !== dragId) return
    if (frame) {
        cancelAnimationFrame(frame)
        update()
    }
    dragging.value = false
    grab = 0
    if (e.pointerType === 'touch') pointer.value = null
    emit('dragend')
    showBriefly()
}

function onPointerLeave() {
    if (dragging.value) return
    cancelAnimationFrame(frame)
    frame = 0
    pointer.value = null
}

// The handle shows on scroll and hides 1.5s after the last scroll or drag.
const { isPending: recent, start: showBriefly } = useTimeoutFn(() => {}, 1500, {
    immediate: false,
})
useEventListener(window, 'scroll', showBriefly, { passive: true })

// Keyboard steps from the last key's segment while the scroll is still there: it can land a
// fraction of a pixel before `start`, which would repeat the same step.
const keyIndex = ref<number | null>(null)
watch(
    () => props.modelValue,
    v => {
        const s = keyIndex.value === null ? undefined : props.segments[keyIndex.value]
        if (!s || Math.abs(v - s.start) > props.tolerance) keyIndex.value = null
    }
)
watch(
    () => props.segments,
    () => (keyIndex.value = null)
)
const current = computed(() => keyIndex.value ?? segmentAt(props.modelValue))

const KEY_STEPS: Record<string, number> = {
    ArrowUp: -1,
    ArrowDown: 1,
    PageUp: -5,
    PageDown: 5,
    Home: -Infinity,
    End: Infinity,
}

function onKeydown(e: KeyboardEvent) {
    const step = KEY_STEPS[e.key]
    if (step === undefined || !props.segments.length) return
    e.preventDefault()
    const i = Math.min(props.segments.length - 1, Math.max(0, current.value + step))
    keyIndex.value = i
    showBriefly()
    emit('seek', props.segments[i]!.start)
}
</script>

<style scoped>
@layer ui {
    .a-scrubber {
        position: relative;
        height: 100%;
        pointer-events: auto;
        isolation: isolate;
        border-radius: 999px;
        outline: none;
        touch-action: none;
        user-select: none;
        cursor: pointer;
        transition: background-color var(--duration-short) var(--ease-standard);
    }

    .a-scrubber:not(.handle):hover {
        background: var(--color-surface-2);
    }

    .a-scrubber:not(.handle):focus-visible,
    .a-scrubber:focus-visible > .a-scrubber__handle {
        outline: 2px solid var(--color-primary);
        outline-offset: -2px;
    }

    /* Only the track, or the handle while shown, takes pointer events from the covers beneath. */
    .a-scrubber.handle {
        pointer-events: none;
        touch-action: auto;
    }

    .a-scrubber__mark {
        position: absolute;
        inset-inline: 0;
        display: flex;
        align-items: center;
        justify-content: center;
        height: 16px;
        color: var(--color-fg-muted);
        font-size: 11px;
        font-weight: 600;
        line-height: 1;
    }

    .a-scrubber__dot {
        width: 4px;
        height: 4px;
        border-radius: 999px;
        background: var(--color-outline);
    }

    .a-scrubber__thumb {
        position: absolute;
        top: calc(var(--pos) * 100% - 1px);
        inset-inline: 4px;
        height: 2px;
        border-radius: 999px;
        background: var(--color-primary);
    }

    .a-scrubber__handle {
        position: absolute;
        top: calc(var(--pos) * (100% - 44px));
        right: 0;
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        width: 28px;
        height: 44px;
        border-radius: 999px 0 0 999px;
        background: var(--color-surface-3);
        box-shadow: var(--shadow-overlay);
        color: var(--color-fg-muted);
        opacity: 0;
        transition: opacity var(--duration-medium) var(--ease-standard);
    }

    .a-scrubber__handle .a-icon {
        margin-block: -3px;
    }

    .a-scrubber.shown > .a-scrubber__handle,
    .a-scrubber:focus-visible > .a-scrubber__handle {
        opacity: 1;
        pointer-events: auto;
        touch-action: none;
    }

    .a-scrubber__bubble {
        position: absolute;
        z-index: 1;
        top: calc(var(--pointer) * 100%);
        right: calc(100% + 8px);
        translate: 0 -50%;
        padding: 6px 10px;
        border-radius: var(--radius-tooltip);
        background: var(--color-inverse);
        color: var(--color-on-inverse);
        font-size: 13px;
        font-weight: 600;
        white-space: nowrap;
        pointer-events: none;
        animation: a-scrubber-in var(--duration-short) var(--ease-standard);
    }

    .a-scrubber.handle > .a-scrubber__bubble {
        top: calc(var(--pointer) * (100% - 44px) + 22px);
        right: 36px;
    }

    @keyframes a-scrubber-in {
        from {
            opacity: 0;
        }
    }
}
</style>
