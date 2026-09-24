<template>
    <component
        :is="to != null ? RouterLink : interactive ? 'button' : 'span'"
        :to="to"
        :type="interactive && to == null ? 'button' : undefined"
        class="a-chip"
        :class="[
            `variant-${variant}`,
            `tone-${tone}`,
            `size-${size}`,
            { 'a-state a-focus': interactive || to != null },
        ]"
    >
        <AIcon v-if="leadingIcon" :icon="leadingIcon" class="a-chip__icon" />
        <slot />
    </component>
</template>

<script setup lang="ts">
import { computed, useAttrs, type Component } from 'vue'
import { RouterLink, type RouteLocationRaw } from 'vue-router'
import AIcon from './AIcon.vue'

/** A `span`; a `button` when it has a click listener; a link with `to`. */
withDefaults(
    defineProps<{
        variant?: 'tonal' | 'outlined'
        tone?: 'neutral' | 'primary' | 'success' | 'warning' | 'danger' | 'info'
        size?: 'sm' | 'md'
        leadingIcon?: Component
        to?: RouteLocationRaw
    }>(),
    { variant: 'tonal', tone: 'neutral', size: 'md' }
)

const attrs = useAttrs()
const interactive = computed(() => !!attrs.onClick)
</script>

<style scoped>
@layer ui {
    .a-chip {
        --chip-bg: var(--color-surface-4);
        --chip-fg: var(--color-fg);

        display: inline-flex;
        flex: none;
        align-items: center;
        gap: 6px;
        max-width: 100%;
        height: var(--chip-height);
        padding: 0 var(--chip-padding);
        border: 1px solid transparent;
        border-radius: 999px;
        background: var(--chip-bg);
        color: var(--chip-fg);
        font-size: var(--chip-font);
        font-weight: 500;
        line-height: 1;
        white-space: nowrap;
        text-decoration: none;
    }

    button.a-chip,
    a.a-chip {
        cursor: pointer;
    }

    .size-md {
        --chip-height: 32px;
        --chip-padding: 14px;
        --chip-font: 14px;
    }

    .size-sm {
        --chip-height: 24px;
        --chip-padding: 10px;
        --chip-font: 12px;
    }

    .a-chip__icon {
        margin-inline-start: -4px;
        font-size: 1.3em;
    }

    .variant-tonal {
        /* Surface 4 equals the raised surface in dark mode: the rule keeps the chip visible on cards and dialogs. */
        &.tone-neutral {
            border-color: var(--color-outline-variant);
        }

        &.tone-primary {
            --chip-bg: var(--color-primary-container);
            --chip-fg: var(--color-on-primary-container);
        }

        &.tone-success {
            --chip-bg: var(--color-success-container);
            --chip-fg: var(--color-on-success-container);
        }

        &.tone-warning {
            --chip-bg: var(--color-warning-container);
            --chip-fg: var(--color-on-warning-container);
        }

        &.tone-danger {
            --chip-bg: var(--color-error-container);
            --chip-fg: var(--color-on-error-container);
        }

        &.tone-info {
            --chip-bg: var(--color-info-container);
            --chip-fg: var(--color-on-info-container);
        }
    }

    .variant-outlined {
        --chip-bg: var(--color-raised);
        --chip-fg: var(--color-fg-muted);

        border-color: var(--color-outline-variant);

        &.tone-primary {
            --chip-fg: var(--color-primary);
        }

        &.tone-success {
            --chip-fg: var(--color-success);
        }

        &.tone-warning {
            --chip-fg: var(--color-warning);
        }

        &.tone-danger {
            --chip-fg: var(--color-error);
        }

        &.tone-info {
            --chip-fg: var(--color-info);
        }
    }
}
</style>
