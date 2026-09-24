<template>
    <component
        :is="icon"
        class="a-icon"
        :style="
            size != null ? { fontSize: typeof size === 'number' ? `${size}px` : size } : undefined
        "
        :role="label ? 'img' : undefined"
        :aria-label="label"
        :aria-hidden="label ? undefined : 'true'"
    />
</template>

<script setup lang="ts">
import type { Component } from 'vue'

defineProps<{
    icon: Component
    /** Makes the icon meaningful (`role="img"`); leave unset for decorative icons. */
    label?: string
    /** A px number or any CSS length. Defaults to the surrounding font size. */
    size?: number | string
}>()
</script>

<style scoped>
@layer ui {
    .a-icon {
        flex: none;
        width: 1em;
        height: 1em;
    }
}
</style>
