<template>
    <TooltipRoot :disabled="disabled">
        <TooltipTrigger as-child>
            <slot />
        </TooltipTrigger>
        <TooltipPortal to="#overlays">
            <TooltipContent class="a-tooltip" :side="side" :side-offset="6" :collision-padding="8">
                <slot name="content">{{ text }}</slot>
            </TooltipContent>
        </TooltipPortal>
    </TooltipRoot>
</template>

<script setup lang="ts">
import { TooltipContent, TooltipPortal, TooltipRoot, TooltipTrigger } from 'reka-ui'

/**
 * Supplementary text only: tooltips open on mouse hover or keyboard focus, never on touch. Needs
 * `TooltipProvider` (in App.vue), which ignores focus that isn't `:focus-visible`.
 */
withDefaults(
    defineProps<{
        text?: string
        side?: 'top' | 'right' | 'bottom' | 'left'
        disabled?: boolean
    }>(),
    { side: 'bottom' }
)
</script>

<style>
@layer ui {
    .a-tooltip {
        z-index: var(--z-tooltip);
        max-width: min(320px, calc(100vw - 16px));
        padding: 6px 10px;
        border-radius: var(--radius-tooltip);
        background: var(--color-inverse);
        color: var(--color-on-inverse);
        font-size: 13px;
        line-height: 1.35;
        animation: a-tooltip-in var(--duration-short) var(--ease-standard);
    }

    @keyframes a-tooltip-in {
        from {
            opacity: 0;
        }
    }
}
</style>
