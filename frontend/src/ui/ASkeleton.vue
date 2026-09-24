<template>
    <span v-if="shape === 'text' && lines > 1" class="a-skeleton-lines" aria-hidden="true">
        <span
            v-for="i in lines"
            :key="i"
            class="a-skeleton shape-text"
            :style="{ width: i === lines ? '60%' : width }"
        />
    </span>
    <span
        v-else
        class="a-skeleton"
        :class="`shape-${shape}`"
        :style="{ width, height }"
        aria-hidden="true"
    />
</template>

<script setup lang="ts">
/** A loading placeholder. Decorative: announce loading elsewhere (`aria-busy`, ASpinner). */
withDefaults(
    defineProps<{
        width?: string
        height?: string
        shape?: 'rect' | 'text' | 'circle'
        /** Text only: the last line is shorter. */
        lines?: number
    }>(),
    { width: '100%', shape: 'rect', lines: 1 }
)
</script>

<style scoped>
@layer ui {
    .a-skeleton {
        display: block;
        border-radius: 8px;
        background: linear-gradient(
                90deg,
                transparent 30%,
                color-mix(in oklch, var(--color-raised) 60%, transparent) 50%,
                transparent 70%
            )
            0 0 / 300% 100% var(--color-surface-4);
        animation: a-shimmer 1.6s linear infinite;
    }

    .shape-text {
        height: 1em;
        border-radius: 6px;
    }

    .shape-circle {
        aspect-ratio: 1;
        border-radius: 999px;
    }

    .a-skeleton-lines {
        display: flex;
        flex-direction: column;
        gap: 0.5em;
    }

    @keyframes a-shimmer {
        from {
            background-position: 100% 0;
        }

        to {
            background-position: 0 0;
        }
    }

    @media (prefers-reduced-motion: reduce) {
        .a-skeleton {
            animation: none;
        }
    }
}
</style>
