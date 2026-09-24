<template>
    <div
        class="a-checkbox"
        :class="[{ disabled, readonly, invalid: errors.length, compact: hideLabel }, $attrs.class]"
        :style="$attrs.style as StyleValue"
    >
        <label class="a-checkbox__row">
            <span class="a-checkbox__box">
                <input
                    :id="id"
                    ref="input"
                    v-bind="mergeAria(controlAttrs, controlAria)"
                    type="checkbox"
                    class="a-focus"
                    :checked="modelValue"
                    :disabled="disabled"
                    :aria-readonly="readonly || undefined"
                    @click="onClick"
                    @change="onChange"
                />
                <AIcon :icon="indeterminate ? IconMinus : IconCheck" class="a-checkbox__mark" />
            </span>
            <span class="a-checkbox__label" :class="{ 'sr-only': hideLabel }">
                <slot>{{ label }}</slot>
            </span>
        </label>
        <AFieldMessage
            :id="messageId"
            :errors="errors"
            :hint="hint"
            icon
            class="a-checkbox__message"
        />
    </div>
</template>

<script setup lang="ts">
import { nextTick, useTemplateRef, watchEffect, type StyleValue } from 'vue'
import AFieldMessage from './AFieldMessage.vue'
import AIcon from './AIcon.vue'
import { IconCheck, IconMinus } from './icons'
import { mergeAria, useControlAttrs, useFieldIds, type FieldProps } from './useFieldIds'

defineOptions({ inheritAttrs: false })

const props = defineProps<FieldProps & { modelValue: boolean; indeterminate?: boolean }>()

const emit = defineEmits<{
    'update:modelValue': [value: boolean]
    /** The native click, so callers can read modifier keys (shift-click range selection). */
    click: [event: MouseEvent]
}>()

const { id, errors, messageId, controlAria } = useFieldIds(props)
const input = useTemplateRef('input')

const controlAttrs = useControlAttrs()

watchEffect(() => {
    if (input.value) input.value.indeterminate = !!props.indeterminate
})

function onClick(e: MouseEvent) {
    if (props.readonly) return e.preventDefault()
    emit('click', e)
}

// Controlled: the box shows `modelValue`, even if the caller doesn't take the update.
async function onChange(e: Event) {
    emit('update:modelValue', (e.target as HTMLInputElement).checked)
    await nextTick()
    if (input.value) {
        input.value.checked = props.modelValue
        input.value.indeterminate = !!props.indeterminate
    }
}

defineExpose({ focus: () => input.value?.focus() })
</script>

<style scoped>
@layer ui {
    .a-checkbox {
        display: flex;
        flex-direction: column;
    }

    .a-checkbox__row {
        display: flex;
        align-items: center;
        gap: 14px;
        min-height: 44px;
        font-size: 15px;
        cursor: pointer;
    }

    .compact .a-checkbox__row {
        min-height: 0;
        padding: 11px;
    }

    .a-checkbox__box {
        position: relative;
        display: grid;
        flex: none;
        place-items: center;
        width: 18px;
        height: 18px;

        /* State layer: a halo around the box. */
        &::before {
            content: '';
            position: absolute;
            inset: -11px;
            border-radius: 999px;
            background: var(--color-fg);
            opacity: 0;
            pointer-events: none;
            transition: opacity var(--duration-short) var(--ease-standard);
        }
    }

    .a-checkbox:not(.disabled, .readonly) .a-checkbox__row:hover .a-checkbox__box::before {
        opacity: 0.08;
    }

    .a-checkbox:not(.disabled, .readonly) .a-checkbox__row:active .a-checkbox__box::before {
        opacity: 0.12;
    }

    .readonly .a-checkbox__row {
        cursor: default;
    }

    input {
        grid-area: 1 / 1;
        width: 18px;
        height: 18px;
        margin: 0;
        border: 2px solid var(--color-fg-muted);
        border-radius: 4px;
        appearance: none;
        background: transparent;
        cursor: inherit;
        transition:
            background-color var(--duration-short) var(--ease-standard),
            border-color var(--duration-short) var(--ease-standard);

        &:is(:checked, :indeterminate) {
            border-color: var(--color-primary);
            background: var(--color-primary);
        }
    }

    .a-checkbox__mark {
        grid-area: 1 / 1;
        font-size: 16px;
        color: var(--color-on-primary);
        stroke: currentColor;
        stroke-width: 1.2;
        pointer-events: none;
        opacity: 0;
        transform: scale(0.6);
        transition:
            opacity var(--duration-short) var(--ease-standard),
            transform var(--duration-short) var(--ease-standard);
    }

    input:is(:checked, :indeterminate) + .a-checkbox__mark {
        opacity: 1;
        transform: none;
    }

    .invalid input:not(:checked, :indeterminate) {
        border-color: var(--color-error);
    }

    .disabled {
        & .a-checkbox__row {
            cursor: not-allowed;
            color: color-mix(in oklch, var(--color-fg) 38%, transparent);
        }

        & input {
            border-color: color-mix(in oklch, var(--color-fg) 38%, transparent);

            &:is(:checked, :indeterminate) {
                border-color: transparent;
                background: color-mix(in oklch, var(--color-fg) 38%, transparent);
            }
        }
    }

    .a-checkbox__message {
        padding-inline-start: 32px;
    }
}
</style>
