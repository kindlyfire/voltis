<template>
    <div
        class="a-field"
        :class="[`size-${size}`, { invalid: errors.length, disabled, readonly }, $attrs.class]"
        :style="$attrs.style as StyleValue"
    >
        <div ref="box" class="a-field__box" @pointerdown="focusControl">
            <span v-if="$slots.leading" class="a-field__side">
                <slot name="leading" />
            </span>
            <div class="a-field__main">
                <label
                    :for="id"
                    class="a-field__label"
                    :class="{ 'sr-only': hideLabel || size === 'sm' }"
                >
                    {{ label }}
                </label>
                <div class="a-field__row">
                    <slot :id="id" :aria="controlAria" />
                    <span v-if="suffix" class="a-field__suffix">{{ suffix }}</span>
                </div>
            </div>
            <span v-if="loading || errors.length || $slots.trailing" class="a-field__side">
                <ASpinner v-if="loading" size="sm" decorative />
                <!-- In light mode the error ring alone looks like focus. -->
                <AIcon v-else-if="errors.length" :icon="IconAlertCircle" />
                <slot name="trailing" />
            </span>
        </div>
        <AFieldMessage
            :id="messageId"
            :errors="errors"
            :hint="hint"
            :hint-tone="hintTone"
            class="a-field__message"
        />
    </div>
</template>

<script setup lang="ts">
import { useTemplateRef, type StyleValue } from 'vue'
import AFieldMessage from './AFieldMessage.vue'
import AIcon from './AIcon.vue'
import ASpinner from './ASpinner.vue'
import { IconAlertCircle } from './icons'
import { useFieldIds, type FieldProps } from './useFieldIds'

/** The filled field box. Internal: the default slot renders the control with `id` and `aria`. */
defineOptions({ inheritAttrs: false })

const props = defineProps<
    FieldProps & {
        suffix?: string
        loading?: boolean
        size: 'sm' | 'md'
        hintTone?: 'warning'
    }
>()

const { id, errors, messageId, controlAria } = useFieldIds(props)
const box = useTemplateRef('box')

// Clicks on the box's padding focus the control, as with a native label.
function focusControl(e: PointerEvent) {
    if ((e.target as HTMLElement).closest('input, textarea, button, a')) return
    e.preventDefault()
    box.value?.querySelector<HTMLElement>('input, textarea')?.focus()
}
</script>

<style scoped>
@layer ui {
    .a-field {
        display: flex;
        flex-direction: column;
        gap: 4px;
        min-width: 0;
    }

    .a-field__box {
        position: relative;
        display: flex;
        align-items: center;
        gap: 10px;
        min-height: 52px;
        padding: 6px 16px;
        border-radius: var(--radius-field);
        background: var(--color-field);
        color: var(--color-fg);
        cursor: text;
        isolation: isolate;
        transition: box-shadow var(--duration-short) var(--ease-standard);

        &::before {
            content: '';
            position: absolute;
            inset: 0;
            z-index: -1;
            border-radius: inherit;
            background: currentColor;
            opacity: 0;
            pointer-events: none;
            transition: opacity var(--duration-short) var(--ease-standard);
        }
    }

    .size-sm .a-field__box {
        min-height: 40px;
        padding-block: 4px;
    }

    .a-field:not(.disabled, .readonly) .a-field__box:hover::before {
        opacity: 0.06;
    }

    .a-field__box:focus-within {
        box-shadow: inset 0 0 0 1.5px var(--color-primary);
    }

    .a-field__main {
        display: flex;
        flex: 1;
        flex-direction: column;
        gap: 3px;
        min-width: 0;
    }

    .a-field__label {
        font-size: 12px;
        line-height: 16px;
        font-weight: 400;
        color: var(--color-fg-muted);
        cursor: inherit;
    }

    .a-field__box:focus-within .a-field__label {
        font-weight: 600;
        color: var(--color-primary);
    }

    .a-field__row {
        display: flex;
        align-items: baseline;
        gap: 6px;
    }

    .a-field__row :deep(:is(input, textarea)) {
        flex: 1;
        min-width: 0;
        padding: 0;
        border: 0;
        background: transparent;
        color: inherit;
        font: inherit;
        font-size: 15px;
        line-height: 22px;
        outline: none;
        resize: none;

        &::placeholder {
            color: var(--color-fg-muted);
            opacity: 1;
        }
    }

    .a-field__suffix,
    .a-field__side {
        color: var(--color-fg-muted);
    }

    .a-field__suffix {
        font-size: 14px;
    }

    .a-field__side {
        display: flex;
        align-items: center;
        gap: 2px;
        font-size: 20px;
    }

    .a-field__side:last-child:has(button) {
        margin-inline-end: -8px;
    }

    .a-field__message {
        padding-inline: 16px;
    }

    .invalid {
        & .a-field__box,
        & .a-field__box:focus-within {
            box-shadow: inset 0 0 0 1.5px var(--color-error);
        }

        & .a-field__label,
        & .a-field__box:focus-within .a-field__label {
            font-weight: 500;
        }

        & .a-field__label,
        & .a-field__box:focus-within .a-field__label,
        & .a-field__side {
            color: var(--color-error);
        }
    }

    .readonly .a-field__row :deep(:is(input, textarea)) {
        color: var(--color-fg-muted);
    }

    .disabled {
        & .a-field__box {
            cursor: not-allowed;
            background: color-mix(in oklch, var(--color-fg) 6%, transparent);
            color: color-mix(in oklch, var(--color-fg) 38%, transparent);
        }

        & .a-field__label,
        & .a-field__message:not(.tone-warning) {
            opacity: 0.6;
        }

        & .a-field__row :deep(:is(input, textarea))::placeholder {
            color: inherit;
        }
    }
}
</style>
