<template>
    <ATooltip :text="label" :disabled="!tooltip">
        <!-- The tooltip repeats the label: keep Reka from also adding it as a description. -->
        <component
            :is="root.is"
            v-bind="{ ...$attrs, ...rootAttrs }"
            class="a-icon-button a-btn-palette a-state a-focus"
            :class="[`variant-${variant}`, `tone-${tone}`, `size-${size}`, { pressed }]"
            :aria-label="label"
            :aria-pressed="pressed ?? undefined"
            :aria-describedby="$attrs['aria-describedby']"
        >
            <ASpinner v-if="loading" size="inherit" decorative />
            <AIcon v-else :icon="pressed && pressedIcon ? pressedIcon : icon" />
            <!-- The label carries the count, so the badge adds nothing to the name. -->
            <ABadge v-if="badge" size="sm" aria-hidden="true" class="a-icon-button-badge">
                {{ badge }}
            </ABadge>
        </component>
    </ATooltip>
</template>

<script setup lang="ts">
import type { Component } from 'vue'
import ABadge from './ABadge.vue'
import AIcon from './AIcon.vue'
import ASpinner from './ASpinner.vue'
import ATooltip from './ATooltip.vue'
import { useButtonRoot, type ButtonRootProps } from './buttonRoot'

defineOptions({ inheritAttrs: false })

const props = withDefaults(
    defineProps<
        ButtonRootProps & {
            /** Accessible name, and the tooltip text. */
            label: string
            icon: Component
            variant?: 'standard' | 'tonal' | 'filled' | 'outlined'
            tone?: 'primary' | 'neutral' | 'danger'
            size?: 'sm' | 'md' | 'lg'
            /** Makes this a toggle button (`aria-pressed`). */
            pressed?: boolean
            pressedIcon?: Component
            tooltip?: boolean
            /** A short count over the button's corner; put the meaning in `label`. */
            badge?: string
        }
    >(),
    {
        variant: 'standard',
        tone: 'neutral',
        size: 'md',
        type: 'button',
        tooltip: true,
        pressed: undefined,
    }
)

const { root, rootAttrs } = useButtonRoot(props)
</script>

<style scoped>
@layer ui {
    .a-icon-button {
        position: relative;
        display: inline-grid;
        flex: none;
        place-items: center;
        width: var(--btn-size);
        height: var(--btn-size);
        border: 1px solid var(--btn-border);
        border-radius: 999px;
        background: var(--btn-bg);
        color: var(--btn-fg);
        font-size: var(--icon-size);
        cursor: pointer;
        user-select: none;
        transition: background-color var(--duration-short) var(--ease-standard);
    }

    .a-icon-button-badge {
        position: absolute;
        top: -2px;
        right: -2px;
        pointer-events: none;
    }

    .size-sm {
        --btn-size: 32px;
        --icon-size: 20px;
    }

    .size-md {
        --btn-size: 40px;
        --icon-size: 22px;
    }

    .size-lg {
        --btn-size: 48px;
        --icon-size: 24px;
    }

    /* Neutral icons without a container are muted. */
    .a-icon-button.tone-neutral:is(.variant-standard, .variant-outlined):not(
            .pressed,
            :disabled,
            [aria-disabled='true']
        ) {
        --btn-fg: var(--color-fg-muted);
    }

    .pressed:not(:disabled, [aria-disabled='true']) {
        --btn-bg: var(--color-secondary-container);
        --btn-fg: var(--color-on-secondary-container);
    }

    .a-icon-button:disabled,
    .a-icon-button[aria-disabled='true'] {
        cursor: not-allowed;
    }

    .a-icon-button[aria-busy='true'] {
        cursor: progress;
    }

    .a-icon-button :deep(.a-spinner) {
        color: currentColor;
    }
}
</style>
