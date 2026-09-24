<template>
    <fieldset
        class="a-radio-group"
        :class="[{ disabled, readonly, invalid: errors.length }, $attrs.class]"
        :style="$attrs.style as StyleValue"
        role="radiogroup"
        :aria-labelledby="`${id}-legend`"
        :aria-readonly="readonly || undefined"
        v-bind="mergeAria(groupAttrs, controlAria)"
    >
        <legend
            :id="`${id}-legend`"
            class="a-radio-group__legend"
            :class="{ 'sr-only': hideLabel }"
        >
            {{ label }}
        </legend>
        <label
            v-for="(option, i) in items"
            :key="option.value"
            class="a-radio"
            :class="{ disabled: disabled || option.disabled }"
        >
            <span class="a-radio__control">
                <input
                    ref="inputs"
                    v-bind="radioAttrs"
                    type="radio"
                    class="a-focus"
                    :name="(radioAttrs.name as string | undefined) ?? id"
                    :value="option.value"
                    :checked="option.value === modelValue"
                    :disabled="disabled || option.disabled"
                    :aria-describedby="option.description ? `${id}-${i}-description` : undefined"
                    @click="readonly && $event.preventDefault()"
                    @keydown="readonly && $event.key.startsWith('Arrow') && $event.preventDefault()"
                    @change="select(option.value)"
                />
                <span class="a-radio__dot" />
            </span>
            <span class="a-radio__text">
                <span class="a-radio__label">{{ option.label }}</span>
                <span
                    v-if="option.description"
                    :id="`${id}-${i}-description`"
                    class="a-radio__description"
                >
                    {{ option.description }}
                </span>
            </span>
        </label>
        <AFieldMessage
            :id="messageId"
            :errors="errors"
            :hint="hint"
            icon
            class="a-radio-group__message"
        />
    </fieldset>
</template>

<script setup lang="ts" generic="V extends OptionValue">
import { computed, nextTick, useTemplateRef, type StyleValue } from 'vue'
import AFieldMessage from './AFieldMessage.vue'
import { normalizeOptions, type Options, type OptionValue } from './options'
import { mergeAria, useControlAttrs, useFieldIds, type FieldProps } from './useFieldIds'

defineOptions({ inheritAttrs: false })

const props = defineProps<FieldProps & { modelValue: V | null | undefined; options: Options<V> }>()

const emit = defineEmits<{ 'update:modelValue': [value: V] }>()

const { id, errors, messageId, controlAria } = useFieldIds(props)
const items = computed(() => normalizeOptions(props.options))
const inputs = useTemplateRef<HTMLInputElement[]>('inputs')

// `name`, `required` and listeners (a form field's `onBlur`) go to each radio; everything else
// (id, aria-*, data-*) to the fieldset.
const controlAttrs = useControlAttrs()
const isRadioAttr = (key: string) => key === 'name' || key === 'required' || /^on[A-Z]/.test(key)
const groupAttrs = computed(() =>
    Object.fromEntries(Object.entries(controlAttrs.value).filter(([k]) => !isRadioAttr(k)))
)
const radioAttrs = computed(() =>
    Object.fromEntries(Object.entries(controlAttrs.value).filter(([k]) => isRadioAttr(k)))
)

// Controlled: the checked radio follows `modelValue`, even if the caller doesn't take the update.
async function select(value: V) {
    emit('update:modelValue', value)
    await nextTick()
    inputs.value?.forEach(input => (input.checked = input.value === String(props.modelValue)))
}
</script>

<style scoped>
@layer ui {
    .a-radio-group {
        display: flex;
        flex-direction: column;
        min-width: 0;
        margin: 0;
        padding: 0;
        border: 0;
    }

    .a-radio-group__legend {
        margin-bottom: 4px;
        padding: 0;
        color: var(--color-fg-muted);
        font-size: 12px;
    }

    .a-radio {
        display: flex;
        align-items: center;
        gap: 12px;
        min-height: 36px;
        padding-block: 4px;
        font-size: 15px;
        cursor: pointer;
    }

    .a-radio__control {
        position: relative;
        display: grid;
        flex: none;
        place-items: center;
        align-self: flex-start;
        margin-top: 1px;

        &::before {
            content: '';
            position: absolute;
            inset: -10px;
            border-radius: 999px;
            background: var(--color-fg);
            opacity: 0;
            pointer-events: none;
            transition: opacity var(--duration-short) var(--ease-standard);
        }
    }

    .a-radio-group:not(.readonly) .a-radio:not(.disabled):hover .a-radio__control::before {
        opacity: 0.08;
    }

    .a-radio-group:not(.readonly) .a-radio:not(.disabled):active .a-radio__control::before {
        opacity: 0.12;
    }

    .readonly .a-radio {
        cursor: default;
    }

    input {
        grid-area: 1 / 1;
        width: 20px;
        height: 20px;
        margin: 0;
        border: 2px solid var(--color-fg-muted);
        border-radius: 999px;
        appearance: none;
        cursor: inherit;
        transition: border-color var(--duration-short) var(--ease-standard);

        &:checked {
            border-color: var(--color-primary);
        }
    }

    .a-radio__dot {
        grid-area: 1 / 1;
        width: 10px;
        height: 10px;
        border-radius: 999px;
        background: var(--color-primary);
        pointer-events: none;
        transform: scale(0);
        transition: transform var(--duration-short) var(--ease-standard);
    }

    input:checked + .a-radio__dot {
        transform: none;
    }

    .a-radio__text {
        display: flex;
        flex-direction: column;
        gap: 2px;
        min-width: 0;
    }

    .a-radio__description {
        color: var(--color-fg-muted);
        font-size: 13px;
        line-height: 1.45;
    }

    .invalid input:not(:checked) {
        border-color: var(--color-error);
    }

    .a-radio.disabled {
        cursor: not-allowed;
        color: color-mix(in oklch, var(--color-fg) 38%, transparent);

        & .a-radio__description {
            color: inherit;
        }

        & input {
            border-color: currentColor;
        }

        & .a-radio__dot {
            background: currentColor;
        }
    }

    .a-radio-group__message {
        padding-inline-start: 32px;
    }
}
</style>
