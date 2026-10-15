<template>
    <span
        class="a-badge"
        :class="[`size-${size}`, { dot: icon }]"
        :role="label ? 'img' : undefined"
        :aria-label="label"
        :title="label"
    >
        <AIcon v-if="icon" :icon="icon" />
        <slot v-else />
    </span>
</template>

<script setup lang="ts">
import type { Component } from 'vue'
import AIcon from './AIcon.vue'

/**
 * An inverse pill over a cover: a count (default slot), or an icon dot that needs a `label`.
 * `label` replaces the visible content for assistive tech ("12 unread"). `sm` is a compact count
 * for sitting on another control.
 */
withDefaults(defineProps<{ icon?: Component; label?: string; size?: 'md' | 'sm' }>(), {
    size: 'md',
})
</script>

<style scoped>
@layer ui {
    .a-badge {
        display: inline-grid;
        place-items: center;
        min-width: 26px;
        height: 26px;
        padding: 0 9px;
        border-radius: 999px;
        background: var(--color-inverse);
        color: var(--color-on-inverse);
        font-size: 12px;
        font-weight: 600;
        line-height: 1;
        font-variant-numeric: tabular-nums;
    }

    .size-sm {
        min-width: 16px;
        height: 16px;
        padding: 0 4px;
        font-size: 10px;
    }

    .dot {
        width: 26px;
        padding: 0;
        font-size: 15px;
    }

    .dot.size-sm {
        width: 16px;
        padding: 0;
        font-size: 11px;
    }
}
</style>
