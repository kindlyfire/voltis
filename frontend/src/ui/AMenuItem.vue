<template>
    <DropdownMenuItem
        :as="to != null ? RouterLink : 'div'"
        :to="to"
        class="a-menu-item"
        :class="{ 'a-menu-item--danger': tone === 'danger' }"
        :disabled="disabled"
        @select="emit('select', $event)"
    >
        <AIcon v-if="leadingIcon" :icon="leadingIcon" class="a-menu-item__icon" />
        <span class="a-menu-item__label"><slot /></span>
        <span v-if="$slots.trailing" class="a-menu-item__trailing"><slot name="trailing" /></span>
    </DropdownMenuItem>
</template>

<script setup lang="ts">
import { DropdownMenuItem } from 'reka-ui'
import type { Component } from 'vue'
import { RouterLink, type RouteLocationRaw } from 'vue-router'
import AIcon from './AIcon.vue'

withDefaults(
    defineProps<{
        leadingIcon?: Component
        /** Renders the item as a single link, so modified clicks (new tab) work. */
        to?: RouteLocationRaw
        disabled?: boolean
        tone?: 'neutral' | 'danger'
    }>(),
    { tone: 'neutral' }
)

const emit = defineEmits<{ select: [event: Event] }>()
</script>

<style>
@layer ui {
    .a-menu-item {
        display: flex;
        align-items: center;
        gap: 14px;
        min-height: 44px;
        padding: 0 14px;
        border-radius: var(--radius-menu-item);
        color: var(--color-fg);
        font-size: 14px;
        text-decoration: none;
        cursor: pointer;
        outline: none;
        user-select: none;

        &[data-highlighted] {
            background: var(--color-surface-3);
        }

        &:focus-visible {
            outline: 2px solid var(--color-primary);
            outline-offset: -2px;
        }

        &[data-disabled] {
            cursor: not-allowed;
            color: color-mix(in oklch, var(--color-fg) 38%, transparent);
        }
    }

    .a-menu-item--danger {
        color: var(--color-error);
    }

    .a-menu-item__icon {
        font-size: 20px;
        color: var(--color-fg-muted);
    }

    .a-menu-item--danger .a-menu-item__icon {
        color: inherit;
    }

    .a-menu-item__label {
        flex: 1;
        min-width: 0;
    }

    .a-menu-item__trailing {
        color: var(--color-fg-muted);
        font-size: 12px;
    }
}
</style>
