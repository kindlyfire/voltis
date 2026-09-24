<template>
    <div
        class="a-progress"
        :class="[`tone-${tone}`, `variant-${variant}`, { indeterminate }]"
        :style="{ '--thickness': `${thickness}px` }"
        role="progressbar"
        :aria-label="label"
        aria-valuemin="0"
        aria-valuemax="100"
        :aria-valuenow="indeterminate ? undefined : Math.round(fraction * 100)"
        :aria-valuetext="valueText"
    >
        <div
            class="a-progress__fill"
            :style="indeterminate ? undefined : { transform: `scaleX(${fraction})` }"
        />
    </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(
    defineProps<{
        /** From 0 to 1. */
        value?: number
        indeterminate?: boolean
        tone?: 'primary' | 'success' | 'danger'
        /** Height in px. */
        thickness?: number
        /** Accessible name. */
        label?: string
        valueText?: string
        /** `overlay` has a dark translucent track, for use on covers. */
        variant?: 'default' | 'overlay'
    }>(),
    { value: 0, tone: 'primary', thickness: 4, variant: 'default' }
)

const fraction = computed(() => Math.min(1, Math.max(0, props.value)))
</script>

<style scoped>
@layer ui {
    .a-progress {
        position: relative;
        height: var(--thickness);
        overflow: hidden;
        border-radius: 999px;
        background: var(--color-primary-container);
    }

    .tone-success {
        --fill: var(--color-success);
    }

    .tone-danger {
        --fill: var(--color-error);
    }

    .a-progress__fill {
        position: absolute;
        inset: 0;
        border-radius: inherit;
        background: var(--fill, var(--color-primary));
        transform-origin: left;
        transition: transform var(--duration-medium) var(--ease-standard);
    }

    .variant-overlay {
        border-radius: 0;
        background: oklch(0 0 0 / 0.22);

        & .a-progress__fill {
            border-radius: 0;
        }
    }

    .indeterminate .a-progress__fill {
        right: auto;
        width: 40%;
        animation: a-progress-slide 1.4s var(--ease-standard) infinite;
    }

    @keyframes a-progress-slide {
        from {
            transform: translateX(-100%);
        }

        to {
            transform: translateX(250%);
        }
    }

    @media (prefers-reduced-motion: reduce) {
        .indeterminate .a-progress__fill {
            animation-duration: 4s;
        }
    }
}
</style>
