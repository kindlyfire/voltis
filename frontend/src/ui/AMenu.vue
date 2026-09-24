<template>
    <DropdownMenuRoot v-model:open="open">
        <DropdownMenuTrigger as-child>
            <slot name="trigger" />
        </DropdownMenuTrigger>
        <DropdownMenuPortal to="#overlays">
            <DropdownMenuContent
                class="a-menu"
                :side="side"
                :align="align"
                :side-offset="6"
                :collision-padding="8"
            >
                <slot />
            </DropdownMenuContent>
        </DropdownMenuPortal>
    </DropdownMenuRoot>
</template>

<script setup lang="ts">
import {
    DropdownMenuContent,
    DropdownMenuPortal,
    DropdownMenuRoot,
    DropdownMenuTrigger,
} from 'reka-ui'
import { useOverlayLayer } from './overlay'

withDefaults(
    defineProps<{
        side?: 'top' | 'right' | 'bottom' | 'left'
        align?: 'start' | 'center' | 'end'
    }>(),
    { side: 'bottom', align: 'start' }
)

const open = defineModel<boolean>('open', { default: false })
useOverlayLayer('menu', open)
</script>

<style>
@layer ui {
    .a-menu {
        z-index: var(--z-popover);
        display: flex;
        flex-direction: column;
        gap: 2px;
        min-width: 232px;
        max-width: min(320px, calc(100vw - 16px));
        max-height: var(--reka-dropdown-menu-content-available-height);
        overflow-y: auto;
        padding: 6px;
        border-radius: var(--radius-menu);
        background: var(--color-raised);
        color: var(--color-fg);
        box-shadow: var(--shadow-overlay);
        transform-origin: var(--reka-dropdown-menu-content-transform-origin);

        &[data-state='open'] {
            animation: a-menu-in var(--duration-short) var(--ease-standard);
        }
    }
}
</style>
