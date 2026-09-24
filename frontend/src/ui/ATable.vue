<template>
    <div class="a-table" :class="`density-${density}`">
        <table>
            <slot />
        </table>
    </div>
</template>

<script setup lang="ts">
/**
 * Styles a native table (pass thead/tbody/tr/th/td as the slot). Scrolls horizontally when
 * too wide. The head sticks to the top of this wrapper: give it a max height to use that.
 */
withDefaults(defineProps<{ density?: 'default' | 'compact' }>(), { density: 'default' })
</script>

<style scoped>
@layer ui {
    .a-table {
        max-width: 100%;
        overflow: auto;
        overscroll-behavior-x: contain;
    }

    table {
        width: 100%;
        border-collapse: collapse;
        font-size: 14px;
        font-variant-numeric: tabular-nums;
    }

    .a-table :deep(:is(th, td)) {
        height: var(--row-height);
        padding: 0 12px;
        border-bottom: 1px solid var(--color-outline-variant);
        text-align: start;
        vertical-align: middle;
    }

    .a-table :deep(:is(th, td):first-child) {
        padding-inline-start: 16px;
    }

    .a-table :deep(:is(th, td):last-child) {
        padding-inline-end: 16px;
    }

    .a-table :deep(thead th) {
        position: sticky;
        top: 0;
        z-index: 1;
        background: var(--color-raised);
        color: var(--color-fg-muted);
        font-size: 13px;
        font-weight: 600;
        white-space: nowrap;
    }

    .a-table :deep(tbody tr) {
        transition: background-color var(--duration-short) var(--ease-standard);
    }

    .a-table :deep(tbody tr:hover) {
        background: color-mix(in oklch, var(--color-fg) 4%, transparent);
    }

    .a-table :deep(tbody tr:last-child > :is(th, td)) {
        border-bottom: 0;
    }

    .density-default {
        --row-height: 52px;
    }

    .density-compact {
        --row-height: 40px;
    }
}
</style>
