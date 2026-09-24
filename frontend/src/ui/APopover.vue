<template>
    <PopoverRoot v-model:open="open">
        <PopoverTrigger as-child>
            <slot name="trigger" />
        </PopoverTrigger>
        <PopoverPortal to="#overlays">
            <PopoverContent
                class="a-popover"
                :side="side"
                :align="align"
                :side-offset="6"
                :collision-padding="8"
                :aria-label="label"
                :style="{ width: typeof width === 'number' ? `${width}px` : width }"
                @open-auto-focus="onOpenAutoFocus"
            >
                <slot />
            </PopoverContent>
        </PopoverPortal>
    </PopoverRoot>
</template>

<script setup lang="ts">
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from 'reka-ui'
import { provide } from 'vue'
import { focusOwnerKey, useOverlayLayer } from './overlay'

/** A panel that stays open while you interact with it (unlike AMenu). */
withDefaults(
    defineProps<{
        /** Accessible name of the panel. */
        label: string
        side?: 'top' | 'right' | 'bottom' | 'left'
        align?: 'start' | 'center' | 'end'
        /** A px number or any CSS width. */
        width?: number | string
    }>(),
    { side: 'bottom', align: 'start' }
)

const open = defineModel<boolean>('open', { default: false })
useOverlayLayer('popover', open)
provide(focusOwnerKey, true)

// An `[autofocus]` element takes initial focus; otherwise Reka focuses the first focusable.
function onOpenAutoFocus(e: Event) {
    const target = (e.target as HTMLElement).querySelector<HTMLElement>('[autofocus]')
    if (!target) return
    e.preventDefault()
    target.focus({ preventScroll: true })
}
</script>

<style>
@layer ui {
    .a-popover {
        z-index: var(--z-popover);
        min-width: 240px;
        max-width: calc(100vw - 16px);
        max-height: var(--reka-popover-content-available-height);
        overflow-y: auto;
        padding: 16px;
        border-radius: var(--radius-menu);
        background: var(--color-raised);
        color: var(--color-fg);
        box-shadow: var(--shadow-overlay);
        transform-origin: var(--reka-popover-content-transform-origin);
        outline: none;

        &[data-state='open'] {
            animation: a-menu-in var(--duration-short) var(--ease-standard);
        }
    }
}
</style>
