<template>
    <span class="a-avatar" :style="{ '--size': `${size}px` }" aria-hidden="true">
        {{ name.trim().charAt(0).toUpperCase() }}
    </span>
</template>

<script setup lang="ts">
/** The user's initial. Decorative: show the name next to it. */
withDefaults(defineProps<{ name: string; size?: number }>(), { size: 34 })
</script>

<style scoped>
@layer ui {
    .a-avatar {
        display: inline-grid;
        flex: none;
        place-items: center;
        width: var(--size);
        height: var(--size);
        border-radius: 999px;
        background: var(--color-primary-container);
        color: var(--color-on-primary-container);
        font-size: calc(var(--size) * 0.42);
        font-weight: 700;
        line-height: 1;
        user-select: none;
    }
}
</style>
