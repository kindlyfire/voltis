<template>
    <ComboboxRoot
        :open="open"
        :model-value="modelValue"
        :disabled="disabled"
        :class="['a-combobox', $attrs.class]"
        :style="$attrs.style"
        open-on-click
        @update:model-value="select"
        @update:open="setOpen"
    >
        <AField v-bind="fieldProps">
            <template #default="{ id, aria }">
                <ComboboxAnchor as-child :reference="box">
                    <ComboboxInput
                        :id="id"
                        ref="control"
                        v-bind="mergeAria(controlAttrs, aria)"
                        :display-value="labelOf"
                        :placeholder="placeholder ?? (size === 'sm' ? label : undefined)"
                        :readonly="readonly"
                    />
                </ComboboxAnchor>
            </template>
            <template #trailing>
                <AIconButton
                    v-if="showClear"
                    :icon="IconClose"
                    :label="`Clear ${label}`"
                    size="sm"
                    :tooltip="false"
                    @click="clear"
                />
                <ComboboxTrigger v-if="!readonly" class="a-combobox__trigger" :disabled="disabled">
                    <AIcon :icon="IconChevronDown" />
                </ComboboxTrigger>
            </template>
        </AField>
        <ComboboxPortal to="#overlays">
            <ComboboxContent
                position="popper"
                class="a-listbox"
                :style="{ minWidth: `${box?.offsetWidth ?? 0}px` }"
                :side-offset="4"
                :collision-padding="8"
            >
                <ComboboxViewport>
                    <ComboboxEmpty class="a-combobox__empty">{{ emptyText }}</ComboboxEmpty>
                    <ComboboxItem
                        v-for="option in items"
                        :key="option.value"
                        :value="option.value"
                        :text-value="option.label"
                        :disabled="option.disabled"
                        class="a-option"
                    >
                        <AIcon v-if="option.icon" :icon="option.icon" class="a-option__icon" />
                        <span class="a-option__text">
                            <slot name="option" :option="option">{{ option.label }}</slot>
                        </span>
                        <ComboboxItemIndicator class="a-option__check">
                            <AIcon :icon="IconCheck" />
                        </ComboboxItemIndicator>
                    </ComboboxItem>
                </ComboboxViewport>
            </ComboboxContent>
        </ComboboxPortal>
    </ComboboxRoot>
</template>

<script setup lang="ts" generic="V extends OptionValue">
import {
    ComboboxAnchor,
    ComboboxContent,
    ComboboxEmpty,
    ComboboxInput,
    ComboboxItem,
    ComboboxItemIndicator,
    ComboboxPortal,
    ComboboxRoot,
    ComboboxTrigger,
    ComboboxViewport,
} from 'reka-ui'
import AField from './AField.vue'
import AIcon from './AIcon.vue'
import AIconButton from './AIconButton.vue'
import { IconCheck, IconChevronDown, IconClose } from './icons'
import type { Option, Options, OptionValue } from './options'
import { mergeAria, useControlAttrs } from './useFieldIds'
import { useSelectField, type SelectFieldProps } from './useSelectField'

/** A select with client-side filtering as you type. */
defineOptions({ inheritAttrs: false })

const props = withDefaults(
    defineProps<
        SelectFieldProps & {
            modelValue: V | null | undefined
            options: Options<V>
            emptyText?: string
        }
    >(),
    { size: 'md', emptyText: 'No matches' }
)

const emit = defineEmits<{ 'update:modelValue': [value: V | null] }>()

defineSlots<{ option?(props: { option: Option<V> }): unknown }>()

const { open, setOpen, items, fieldProps, showClear, box, select, clear } = useSelectField(
    props,
    emit
)
const labelOf = (value: unknown) => items.value.find(o => o.value === value)?.label ?? ''
const controlAttrs = useControlAttrs()
</script>

<style>
@layer ui {
    .a-combobox__trigger {
        display: grid;
        place-items: center;
        width: 32px;
        height: 32px;
        border: 0;
        border-radius: 999px;
        background: none;
        color: inherit;
        cursor: pointer;

        &:disabled {
            cursor: not-allowed;
        }
    }

    .a-combobox__empty {
        padding: 12px 14px;
        color: var(--color-fg-muted);
        font-size: 14px;
    }
}
</style>
