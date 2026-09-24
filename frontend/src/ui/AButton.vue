<template>
    <component
        :is="root.is"
        v-bind="rootAttrs"
        :aria-keyshortcuts="root.is === 'button' && type === 'submit' ? SUBMIT_KEYS : undefined"
        class="a-button a-btn-palette a-state a-focus"
        :class="[
            `variant-${variant}`,
            `tone-${tone}`,
            `size-${size}`,
            {
                'is-block': block,
                loading,
                overlaid: loading && !leadingIcon,
                'has-leading': leadingIcon,
            },
        ]"
    >
        <ASpinner v-if="loading && leadingIcon" size="inherit" decorative />
        <AIcon v-else-if="leadingIcon" :icon="leadingIcon" class="a-button__icon" />
        <span class="a-button__label"><slot /></span>
        <AIcon v-if="trailingIcon" :icon="trailingIcon" class="a-button__icon" />
        <template v-if="href && !disabled">
            <AIcon :icon="IconOpenInNew" class="a-button__icon" />
            <span class="sr-only">(opens in a new tab)</span>
        </template>
        <span v-if="loading && !leadingIcon" class="a-button__overlay">
            <ASpinner size="inherit" decorative />
        </span>
    </component>
</template>

<script setup lang="ts">
import type { Component } from 'vue'
import AIcon from './AIcon.vue'
import ASpinner from './ASpinner.vue'
import { useButtonRoot, type ButtonRootProps } from './buttonRoot'
import { IconOpenInNew } from './icons'

const props = withDefaults(
    defineProps<
        ButtonRootProps & {
            variant?: 'filled' | 'tonal' | 'outlined' | 'text'
            tone?: 'primary' | 'neutral' | 'danger'
            size?: 'sm' | 'md' | 'lg'
            leadingIcon?: Component
            trailingIcon?: Component
            block?: boolean
        }
    >(),
    { variant: 'filled', tone: 'primary', size: 'md', type: 'button' }
)

const { root, rootAttrs } = useButtonRoot(props)
// Handled app-wide by `useSubmitShortcut`.
const SUBMIT_KEYS = 'Control+Enter Meta+Enter'
</script>

<style scoped>
@layer ui {
    .a-button {
        --btn-padding: 24px;

        display: inline-flex;
        align-items: center;
        justify-content: center;
        gap: 8px;
        height: var(--btn-height);
        padding-inline: var(--btn-padding);
        border: 1px solid var(--btn-border);
        border-radius: 999px;
        background: var(--btn-bg);
        color: var(--btn-fg);
        font-size: 14px;
        font-weight: 600;
        line-height: 1;
        white-space: nowrap;
        cursor: pointer;
        user-select: none;
        text-decoration: none;
        transition:
            box-shadow var(--duration-short) var(--ease-standard),
            background-color var(--duration-short) var(--ease-standard);
    }

    .size-sm {
        --btn-height: 32px;
        font-size: 13px;
    }

    .size-md {
        --btn-height: 40px;
    }

    .size-lg {
        --btn-height: 44px;
    }

    .size-lg.variant-filled {
        --btn-padding: 26px;
    }

    .size-sm:not(.variant-text) {
        --btn-padding: 16px;
    }

    /* A leading icon carries visual weight of its own. */
    .has-leading {
        padding-inline-start: calc(var(--btn-padding) - 4px);
    }

    .is-block {
        display: flex;
        width: 100%;
    }

    .a-button__icon {
        font-size: 20px;
    }

    .size-sm .a-button__icon {
        font-size: 18px;
    }

    .variant-filled:not(:disabled, [aria-disabled='true']):hover {
        box-shadow: var(--shadow-button-hover);
    }

    .variant-tonal,
    .variant-outlined {
        --btn-padding: 20px;
    }

    .variant-text {
        --btn-padding: 14px;
    }

    .a-button:disabled,
    .a-button[aria-disabled='true'] {
        cursor: not-allowed;
    }

    .loading {
        cursor: progress;
    }

    /* The spinner overlays the label: keep its width and accessible name, hide its pixels. */
    .overlaid > :not(.a-button__overlay) {
        opacity: 0;
    }

    .a-button__overlay {
        position: absolute;
        inset: 0;
        display: grid;
        place-items: center;
        font-size: 1.3em;
    }

    .a-button :deep(.a-spinner) {
        color: currentColor;
    }
}
</style>
