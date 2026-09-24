<template>
    <AField v-bind="fieldProps" :class="['a-select', $attrs.class]" :style="$attrs.style">
        <template #default="{ id, aria }">
            <SelectRoot
                :open="open"
                :model-value="modelValue"
                :disabled="disabled"
                @update:model-value="select"
                @update:open="setOpen"
            >
                <SelectTrigger
                    :id="id"
                    ref="control"
                    v-bind="mergeAria(controlAttrs, aria)"
                    class="a-select__trigger"
                    :aria-readonly="readonly || undefined"
                >
                    <span v-if="selected" class="a-select__value">
                        <slot name="option" :option="selected">{{ selected.label }}</slot>
                    </span>
                    <span v-else class="a-select__placeholder">
                        {{ placeholder ?? (size === 'sm' ? label : '') }}
                    </span>
                </SelectTrigger>
                <SelectPortal to="#overlays">
                    <SelectContent
                        position="popper"
                        class="a-listbox"
                        :reference="box"
                        :style="{ minWidth: `${box?.offsetWidth ?? 0}px` }"
                        :side-offset="4"
                        :collision-padding="8"
                    >
                        <SelectViewport>
                            <SelectItem
                                v-for="option in items"
                                :key="option.value"
                                :value="option.value"
                                :text-value="option.label"
                                :disabled="option.disabled"
                                class="a-option"
                            >
                                <AIcon
                                    v-if="option.icon"
                                    :icon="option.icon"
                                    class="a-option__icon"
                                />
                                <SelectItemText class="a-option__text">
                                    <slot name="option" :option="option">{{ option.label }}</slot>
                                </SelectItemText>
                                <SelectItemIndicator class="a-option__check">
                                    <AIcon :icon="IconCheck" />
                                </SelectItemIndicator>
                            </SelectItem>
                        </SelectViewport>
                    </SelectContent>
                </SelectPortal>
            </SelectRoot>
        </template>
        <template #trailing>
            <span v-if="showClear" class="a-select__actions">
                <AIconButton
                    :icon="IconClose"
                    :label="`Clear ${label}`"
                    size="sm"
                    :tooltip="false"
                    @click="clear"
                />
            </span>
            <AIcon v-if="!readonly" :icon="IconChevronDown" class="a-select__chevron" />
        </template>
    </AField>
</template>

<script setup lang="ts" generic="V extends OptionValue">
import {
    SelectContent,
    SelectItem,
    SelectItemIndicator,
    SelectItemText,
    SelectPortal,
    SelectRoot,
    SelectTrigger,
    SelectViewport,
} from 'reka-ui'
import { computed } from 'vue'
import AField from './AField.vue'
import AIcon from './AIcon.vue'
import AIconButton from './AIconButton.vue'
import { IconCheck, IconChevronDown, IconClose } from './icons'
import type { Option, Options, OptionValue } from './options'
import { mergeAria, useControlAttrs } from './useFieldIds'
import { useSelectField, type SelectFieldProps } from './useSelectField'

defineOptions({ inheritAttrs: false })

const props = withDefaults(
    defineProps<SelectFieldProps & { modelValue: V | null | undefined; options: Options<V> }>(),
    { size: 'md' }
)

const emit = defineEmits<{ 'update:modelValue': [value: V | null] }>()

defineSlots<{ option?(props: { option: Option<V> }): unknown }>()

const { open, setOpen, items, fieldProps, showClear, box, select, clear, control } = useSelectField(
    props,
    emit
)
const selected = computed(() => items.value.find(o => o.value === props.modelValue))
const controlAttrs = useControlAttrs()

defineExpose({ focus: () => control.value?.$el.focus() })
</script>

<!-- Unscoped: Reka renders the trigger and the portalled popup. -->
<style>
@layer ui {
    .a-select .a-field__box {
        cursor: pointer;
    }

    .a-select.disabled .a-field__box {
        cursor: not-allowed;
    }

    .a-select.readonly .a-field__box {
        cursor: default;
    }

    .a-select__trigger {
        flex: 1;
        min-width: 0;
        padding: 0;
        border: 0;
        background: none;
        color: inherit;
        font: inherit;
        font-size: 15px;
        line-height: 22px;
        text-align: start;
        cursor: inherit;
        outline: none;

        /* The whole box opens the select. */
        &::after {
            content: '';
            position: absolute;
            inset: 0;
            border-radius: inherit;
        }
    }

    .a-select__value,
    .a-select__placeholder {
        display: block;
        min-height: 22px;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    .a-select__placeholder {
        color: var(--color-fg-muted);
    }

    .a-select.disabled .a-select__placeholder {
        color: inherit;
    }

    .a-select.readonly .a-select__value {
        color: var(--color-fg-muted);
    }

    .a-select__actions {
        position: relative;
        z-index: 1;
        display: flex;
    }

    .a-select__chevron {
        pointer-events: none;
        transition: transform var(--duration-short) var(--ease-standard);
    }

    .a-select:has([data-state='open']) .a-select__chevron {
        transform: rotate(180deg);
    }
}
</style>
