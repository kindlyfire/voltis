<template>
    <ToggleGroupRoot
        type="single"
        class="a-segmented"
        :class="[`size-${size}`, { 'is-block': block }]"
        :model-value="modelValue"
        :aria-label="label"
        :disabled="disabled"
        @update:model-value="select"
    >
        <ToggleGroupItem
            v-for="option in items"
            :key="option.value"
            :value="option.value"
            :disabled="option.disabled"
            class="a-segmented__item a-state"
        >
            <AIcon
                v-if="option.value === modelValue"
                :icon="IconCheck"
                class="a-segmented__check"
            />
            <AIcon v-else-if="option.icon" :icon="option.icon" class="a-segmented__icon" />
            {{ option.label }}
        </ToggleGroupItem>
    </ToggleGroupRoot>
</template>

<script setup lang="ts" generic="V extends OptionValue">
import { ToggleGroupItem, ToggleGroupRoot, type AcceptableValue } from 'reka-ui'
import { computed } from 'vue'
import AIcon from './AIcon.vue'
import { IconCheck } from './icons'
import { normalizeOptions, type Options, type OptionValue } from './options'

/**
 * A single-choice button group (always one selected). A model value outside the options
 * selects nothing; the first enabled option stays tabbable.
 */
const props = withDefaults(
    defineProps<{
        modelValue: V | null | undefined
        options: Options<V>
        /** Accessible name of the group. */
        label: string
        size?: 'sm' | 'md'
        /** Full width, equal segments. */
        block?: boolean
        disabled?: boolean
    }>(),
    { size: 'md' }
)

const emit = defineEmits<{ 'update:modelValue': [value: V] }>()

const items = computed(() => normalizeOptions(props.options))

// Reka emits `undefined` when the selected item is clicked again: stay selected.
function select(value: AcceptableValue | AcceptableValue[]) {
    if (value != null && !Array.isArray(value)) emit('update:modelValue', value as V)
}
</script>

<!-- Unscoped: the items are Reka's elements. -->
<style>
@layer ui {
    .a-segmented {
        display: inline-flex;
        /* Not stretched by a flex column parent. */
        width: fit-content;
        max-width: 100%;
        overflow: hidden;
        border: 1px solid var(--color-outline);
        border-radius: 999px;

        &.is-block {
            display: flex;
            width: 100%;
        }
    }

    .a-segmented__item {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        gap: 6px;
        height: var(--seg-height);
        padding: 0 16px;
        border: 0;
        background: transparent;
        color: var(--color-fg);
        font-size: 14px;
        font-weight: 600;
        white-space: nowrap;
        cursor: pointer;
        outline: none;

        & + & {
            border-left: 1px solid var(--color-outline);
        }

        /* Round the outer ends, so the inset focus ring follows the pill. */
        &:first-child {
            border-radius: 999px 0 0 999px;
        }

        &:last-child {
            border-radius: 0 999px 999px 0;
        }

        &:only-child {
            border-radius: 999px;
        }

        &:focus-visible {
            outline: 2px solid var(--color-primary);
            outline-offset: -3px;
        }

        &[data-state='on'] {
            padding-inline-start: 12px;
            background: var(--color-secondary-container);
            color: var(--color-on-secondary-container);
        }

        &[data-disabled] {
            cursor: not-allowed;
            color: color-mix(in oklch, var(--color-fg) 38%, transparent);
        }

        &[data-disabled][data-state='on'] {
            background: color-mix(in oklch, var(--color-fg) 12%, transparent);
        }
    }

    .a-segmented.size-md {
        --seg-height: 38px;
    }

    .a-segmented.size-sm {
        --seg-height: 32px;

        & .a-segmented__item {
            padding-inline: 12px;
            font-size: 13px;

            &[data-state='on'] {
                padding-inline-start: 8px;
            }
        }
    }

    .a-segmented.is-block .a-segmented__item {
        flex: 1;
        min-width: 0;
    }

    .a-segmented__check {
        font-size: 18px;
        stroke: currentColor;
        stroke-width: 0.6;
    }

    .a-segmented__icon {
        font-size: 18px;
    }
}
</style>
