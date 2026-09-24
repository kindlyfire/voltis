<template>
    <span
        class="a-spinner"
        :class="`size-${size}`"
        :role="decorative ? undefined : 'status'"
        :aria-hidden="decorative ? 'true' : undefined"
    >
        <svg viewBox="0 0 24 24" aria-hidden="true">
            <circle cx="12" cy="12" r="9.5" />
        </svg>
        <span v-if="!decorative" class="sr-only">{{ label }}</span>
    </span>
</template>

<script setup lang="ts">
withDefaults(
    defineProps<{
        /** `inherit` follows the font size (1em), for use inside buttons and fields. */
        size?: 'inherit' | 'sm' | 'md' | 'lg'
        label?: string
        decorative?: boolean
    }>(),
    { size: 'md', label: 'Loading' }
)
</script>

<style scoped>
@layer ui {
    .a-spinner {
        display: inline-flex;
        flex: none;
        color: var(--color-primary);
        width: 1em;
        height: 1em;
    }

    .size-sm {
        font-size: 16px;
    }

    .size-md {
        font-size: 24px;
    }

    .size-lg {
        font-size: 56px;
    }

    svg {
        width: 100%;
        height: 100%;
        animation: a-spin 0.9s linear infinite;
    }

    circle {
        fill: none;
        stroke: currentColor;
        stroke-width: 2.5;
        stroke-linecap: round;
        stroke-dasharray: 42 60;
    }

    @media (prefers-reduced-motion: reduce) {
        /* Still conveys progress, but slowly. */
        svg {
            animation-duration: 2.4s;
        }
    }

    @keyframes a-spin {
        to {
            transform: rotate(360deg);
        }
    }
}
</style>
