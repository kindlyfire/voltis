<template>
    <AField v-bind="fieldProps" :class="$attrs.class" :style="$attrs.style">
        <template v-if="leadingIcon" #leading>
            <AIcon :icon="leadingIcon" />
        </template>
        <template #default="{ id, aria }">
            <component
                :is="multiline ? 'textarea' : 'input'"
                ref="control"
                v-bind="mergeAria(controlAttrs, aria)"
                :id="id"
                :type="multiline ? undefined : type"
                :rows="multiline ? (rows ?? 3) : undefined"
                :class="{ 'auto-grow': multiline && autoGrow }"
                :value="modelValue ?? ''"
                :placeholder="placeholder ?? (size === 'sm' ? label : undefined)"
                :disabled="disabled"
                :readonly="readonly"
                :autofocus="autofocus"
                @input="!($event as InputEvent).isComposing && onInput($event)"
                @compositionend="onInput"
            />
        </template>
        <template v-if="showClear || $slots.trailing" #trailing>
            <AIconButton
                v-if="showClear"
                :icon="IconClose"
                :label="`Clear ${label}`"
                size="sm"
                :tooltip="false"
                @click="clear"
            />
            <slot name="trailing" />
        </template>
    </AField>
</template>

<script setup lang="ts">
import { computed, inject, onMounted, useTemplateRef, type Component } from 'vue'
import AField from './AField.vue'
import AIcon from './AIcon.vue'
import AIconButton from './AIconButton.vue'
import { IconClose } from './icons'
import { focusOwnerKey } from './overlay'
import { mergeAria, useControlAttrs, type FieldProps } from './useFieldIds'

defineOptions({ inheritAttrs: false })

type Common = FieldProps & {
    /** Multiline only. */
    rows?: number
    /** Multiline only: grows with the content from `rows` up. */
    autoGrow?: boolean
    placeholder?: string
    clearable?: boolean
    leadingIcon?: Component
    suffix?: string
    loading?: boolean
    size?: 'sm' | 'md'
    autofocus?: boolean
    /** Colors the hint, for a caution about the current value. */
    hintTone?: 'warning'
}
// The model follows the input mode: a string for text, a number (null when empty) for `type="number"`.
// The handler types only type the call sites; updates go through `emit`, which also handles `.once`
// and several listeners (hence the array).
type Handler<T> = ((value: T) => void) | ((value: T) => void)[]
type TextModel = {
    type?: 'text' | 'password' | 'email' | 'url' | 'search'
    /** Renders a textarea. */
    multiline?: boolean
    modelValue: string | null | undefined
    'onUpdate:modelValue'?: Handler<string>
}
type NumberModel = {
    type: 'number'
    multiline?: false
    modelValue: number | null | undefined
    'onUpdate:modelValue'?: Handler<number | null>
}

// No `withDefaults`: with it, vue-tsc stops checking this union's props at call sites.
const props = defineProps<Common & (TextModel | NumberModel)>()

const control = useTemplateRef<HTMLInputElement | HTMLTextAreaElement>('control')

const fieldProps = computed(() => {
    const {
        label,
        hideLabel,
        hint,
        hintTone,
        error,
        disabled,
        readonly,
        id,
        suffix,
        loading,
        size,
    } = props
    return {
        label,
        hideLabel,
        hint,
        hintTone,
        error,
        disabled,
        readonly,
        id,
        suffix,
        loading,
        size: size ?? 'md',
    }
})

// Native attributes (name, autocomplete, required, onBlur, ...) go to the control.
const controlAttrs = useControlAttrs()

const showClear = computed(
    () =>
        props.clearable &&
        !props.disabled &&
        !props.readonly &&
        props.modelValue != null &&
        props.modelValue !== ''
)

const emit = defineEmits(['update:modelValue'])

function update(raw: string) {
    if (props.type === 'number') emit('update:modelValue', raw === '' ? null : Number(raw))
    else emit('update:modelValue', raw)
}

// Skipped mid-composition (IME): `compositionend` delivers the final text.
function onInput(e: Event) {
    update((e.target as HTMLInputElement).value)
}

function clear() {
    update('')
    control.value?.focus()
}

// Dialogs and popovers own initial focus (they pick the `[autofocus]` element); elsewhere, e.g. a
// route-level form, the field focuses itself, since `autofocus` only applies on page load.
const inFocusOwner = inject(focusOwnerKey, false)
onMounted(() => {
    if (props.autofocus && !inFocusOwner) control.value?.focus()
})

defineExpose({ focus: () => control.value?.focus() })
</script>

<style scoped>
@layer ui {
    textarea.auto-grow {
        field-sizing: content;
        min-height: calc(v-bind('rows ?? 3') * 22px);
        max-height: 50vh;
    }
}
</style>
