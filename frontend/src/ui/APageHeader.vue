<template>
    <header class="a-page-header">
        <h1 class="a-page-header__title">
            <slot name="title">{{ title }}</slot>
        </h1>
        <div v-if="$slots.actions || $slots.meta" class="a-page-header__toolbar">
            <slot name="actions" />
            <span v-if="$slots.meta" class="a-page-header__meta"><slot name="meta" /></span>
        </div>
    </header>
</template>

<script setup lang="ts">
/** The page's h1, with an optional toolbar row (icon buttons, then meta text) below it. */
defineProps<{ title?: string }>()
</script>

<style scoped>
@layer ui {
    .a-page-header {
        display: flex;
        flex-direction: column;
        gap: 6px;
    }

    .a-page-header__title {
        margin-top: 8px;
        font-family: var(--font-display);
        font-size: 40px;
        font-weight: 600;
        line-height: 1.15;
        overflow-wrap: anywhere;
    }

    .a-page-header__toolbar {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 4px;
        margin-inline-start: -8px;
    }

    .a-page-header__meta {
        margin-inline-start: 10px;
        color: var(--color-fg-muted);
        font-size: 14px;
    }

    @media (width < 40rem) {
        .a-page-header__title {
            font-size: 32px;
        }
    }
}
</style>
