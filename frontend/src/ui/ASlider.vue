<template>
    <div class="a-slider" :class="[{ disabled }, $attrs.class]" :style="$attrs.style as StyleValue">
        <div v-if="!hideLabel || showValue" class="a-slider__header">
            <span :class="{ 'sr-only': hideLabel }">{{ label }}</span>
            <span v-if="showValue" class="a-slider__value" aria-hidden="true">{{ valueText }}</span>
        </div>
        <SliderRoot
            class="a-slider__root"
            :model-value="[modelValue]"
            :min="min"
            :max="max"
            :step="step"
            :disabled="disabled"
            @update:model-value="v => v && emit('update:modelValue', v[0]!)"
            @value-commit="v => emit('commit', v[0]!)"
        >
            <SliderTrack class="a-slider__track">
                <SliderRange class="a-slider__range" />
            </SliderTrack>
            <SliderThumb
                v-bind="controlAttrs"
                class="a-slider__thumb"
                :aria-label="label"
                :aria-valuetext="formatValue ? valueText : undefined"
            />
        </SliderRoot>
    </div>
</template>

<script setup lang="ts">
import { SliderRange, SliderRoot, SliderThumb, SliderTrack } from 'reka-ui'
import { computed, type StyleValue } from 'vue'
import { useControlAttrs } from './useFieldIds'

defineOptions({ inheritAttrs: false })

const props = withDefaults(
    defineProps<{
        modelValue: number
        label: string
        hideLabel?: boolean
        min?: number
        max?: number
        step?: number
        disabled?: boolean
        /** Drives the visible value and `aria-valuetext`. */
        formatValue?: (value: number) => string
        showValue?: boolean
    }>(),
    { min: 0, max: 100, step: 1 }
)

const emit = defineEmits<{
    'update:modelValue': [value: number]
    /** The value at the end of a drag or key press. */
    commit: [value: number]
}>()

const valueText = computed(() => props.formatValue?.(props.modelValue) ?? String(props.modelValue))

const controlAttrs = useControlAttrs()
</script>

<!-- Unscoped: the thumb and track are Reka's elements. -->
<style>
@layer ui {
    .a-slider {
        display: flex;
        flex-direction: column;
        gap: 2px;
    }

    .a-slider__header {
        display: flex;
        justify-content: space-between;
        gap: 12px;
        font-size: 13px;
    }

    .a-slider__value {
        color: var(--color-fg-muted);
        font-variant-numeric: tabular-nums;
    }

    .a-slider__root {
        position: relative;
        display: flex;
        align-items: center;
        height: 36px;
        touch-action: none;
        user-select: none;
        cursor: pointer;
    }

    .a-slider__track {
        position: relative;
        flex: 1;
        height: 4px;
        overflow: hidden;
        border-radius: 999px;
        background: var(--color-primary-container);
    }

    .a-slider__range {
        position: absolute;
        height: 100%;
        background: var(--color-primary);
    }

    .a-slider__thumb {
        display: block;
        width: 20px;
        height: 20px;
        border-radius: 999px;
        background: var(--color-primary);
        box-shadow: 0 1px 2px oklch(0 0 0 / 0.25);
        outline: none;
        transition: box-shadow var(--duration-short) var(--ease-standard);

        &:hover {
            box-shadow:
                0 0 0 8px color-mix(in oklch, var(--color-primary) 12%, transparent),
                0 1px 2px oklch(0 0 0 / 0.25);
        }

        &:focus-visible {
            outline: 2px solid var(--color-primary);
            outline-offset: 2px;
        }
    }

    .a-slider.disabled {
        color: color-mix(in oklch, var(--color-fg) 38%, transparent);

        & .a-slider__root {
            cursor: not-allowed;
        }

        & .a-slider__value {
            color: inherit;
        }

        & .a-slider__track {
            background: color-mix(in oklch, var(--color-fg) 12%, transparent);
        }

        & :is(.a-slider__range, .a-slider__thumb) {
            background: color-mix(in oklch, var(--color-fg) 38%, var(--color-bg));
            box-shadow: none;
        }
    }
}
</style>
