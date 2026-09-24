<template>
    <div
        class="a-switch"
        :class="[{ disabled, readonly, invalid: errors.length }, $attrs.class]"
        :style="$attrs.style as StyleValue"
    >
        <label class="a-switch__row">
            <span class="a-switch__text">
                <span class="a-switch__label" :class="{ 'sr-only': hideLabel }">{{ label }}</span>
                <span v-if="description" :id="descriptionId" class="a-switch__description">
                    {{ description }}
                </span>
            </span>
            <span class="a-switch__track">
                <input
                    :id="id"
                    ref="input"
                    v-bind="mergeAria(controlAttrs, controlAria, description && descriptionId)"
                    type="checkbox"
                    role="switch"
                    class="a-focus"
                    :checked="modelValue"
                    :disabled="disabled"
                    :aria-readonly="readonly || undefined"
                    @click="readonly && $event.preventDefault()"
                    @change="onChange"
                />
                <span class="a-switch__thumb" />
            </span>
        </label>
        <AFieldMessage
            :id="messageId"
            :errors="errors"
            :hint="hint"
            icon
            class="a-switch__message"
        />
    </div>
</template>

<script setup lang="ts">
import { computed, nextTick, useTemplateRef, type StyleValue } from 'vue'
import AFieldMessage from './AFieldMessage.vue'
import { mergeAria, useControlAttrs, useFieldIds, type FieldProps } from './useFieldIds'

/** A settings row: label (and description) on the left, the switch on the right. */
defineOptions({ inheritAttrs: false })

const props = defineProps<FieldProps & { modelValue: boolean; description?: string }>()

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

const { id, errors, messageId, controlAria } = useFieldIds(props)
const descriptionId = computed(() => `${id.value}-description`)
const input = useTemplateRef('input')

const controlAttrs = useControlAttrs()

async function onChange(e: Event) {
    emit('update:modelValue', (e.target as HTMLInputElement).checked)
    await nextTick()
    if (input.value) input.value.checked = props.modelValue
}
</script>

<style scoped>
@layer ui {
    .a-switch__row {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 16px;
        min-height: 48px;
        cursor: pointer;
    }

    .a-switch__text {
        display: flex;
        flex-direction: column;
        gap: 2px;
        min-width: 0;
    }

    .a-switch__label {
        font-size: 15px;
    }

    .a-switch__description {
        color: var(--color-fg-muted);
        font-size: 13px;
        line-height: 1.45;
    }

    .a-switch__track {
        position: relative;
        display: grid;
        flex: none;
    }

    input {
        width: 44px;
        height: 26px;
        margin: 0;
        border-radius: 999px;
        appearance: none;
        background: var(--color-surface-4);
        /* Not in the design: without it the off track vanishes on raised cards in dark mode. */
        box-shadow: inset 0 0 0 1px var(--color-outline);
        cursor: inherit;
        transition:
            background-color 0.2s var(--ease-standard),
            box-shadow 0.2s var(--ease-standard);

        &:checked {
            background: var(--color-primary);
            box-shadow: none;
        }
    }

    .a-switch__thumb {
        --size: 18px;

        position: absolute;
        top: calc((26px - var(--size)) / 2);
        left: calc((26px - var(--size)) / 2);
        width: var(--size);
        height: var(--size);
        border-radius: 999px;
        background: white;
        box-shadow: 0 1px 2px oklch(0 0 0 / 0.25);
        pointer-events: none;
        transition: all 0.22s var(--ease-standard);
    }

    input:checked + .a-switch__thumb {
        --size: 20px;

        left: calc(44px - 26px + (26px - var(--size)) / 2);
    }

    .readonly .a-switch__row {
        cursor: default;
    }

    .a-switch:not(.readonly) .a-switch__row:hover input:not(:disabled, :checked) {
        background: color-mix(in oklch, var(--color-surface-4), var(--color-fg) 8%);
    }

    .a-switch:not(.readonly) .a-switch__row:hover input:not(:disabled):checked {
        background: color-mix(in oklch, var(--color-primary), var(--color-fg) 10%);
    }

    .invalid input:not(:checked) {
        box-shadow: inset 0 0 0 1.5px var(--color-error);
    }

    .disabled {
        & .a-switch__row {
            cursor: not-allowed;
            color: color-mix(in oklch, var(--color-fg) 38%, transparent);
        }

        & .a-switch__description {
            color: inherit;
        }

        & input {
            background: color-mix(in oklch, var(--color-fg) 12%, transparent);
            box-shadow: none;
        }

        & .a-switch__thumb {
            opacity: 0.6;
        }
    }
}
</style>
