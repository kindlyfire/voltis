<template>
    <ComboboxRoot
        :open="open"
        :model-value="modelValue"
        :disabled="disabled"
        :by="sameValue"
        :ignore-filter="remote"
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
                        @input="search = ($event.target as HTMLInputElement).value"
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
                    <DefineOption v-slot="{ option }">
                        <ComboboxItem
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
                    </DefineOption>
                    <ComboboxEmpty class="a-combobox__empty">{{ emptyText }}</ComboboxEmpty>
                    <!-- The virtualizer's positioning attributes fall through to the item. -->
                    <ComboboxVirtualizer
                        v-if="virtual"
                        v-slot="{ option }"
                        :options="filtered"
                        :estimate-size="OPTION_HEIGHT"
                    >
                        <ReuseOption :option="option" />
                    </ComboboxVirtualizer>
                    <template v-else>
                        <ReuseOption v-for="option in items" :key="option.value" :option="option" />
                    </template>
                </ComboboxViewport>
            </ComboboxContent>
        </ComboboxPortal>
    </ComboboxRoot>
</template>

<script setup lang="ts" generic="V extends OptionValue">
import { createReusableTemplate } from '@vueuse/core'
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
    ComboboxVirtualizer,
    useFilter,
} from 'reka-ui'
import { computed, ref, watch, watchEffect } from 'vue'
import AField from './AField.vue'
import AIcon from './AIcon.vue'
import AIconButton from './AIconButton.vue'
import { IconCheck, IconChevronDown, IconClose } from './icons'
import type { Option, Options, OptionValue } from './options'
import { mergeAria, useControlAttrs } from './useFieldIds'
import { useSelectField, type SelectFieldProps } from './useSelectField'

/**
 * A select with client-side filtering as you type, or `remote`, filtered by the parent through
 * `v-model:search`. Long option lists are virtualized, with one-line options.
 */
defineOptions({ inheritAttrs: false })

const props = withDefaults(
    defineProps<
        SelectFieldProps & {
            modelValue: V | null | undefined
            options: Options<V>
            emptyText?: string
            /** The options already match the search; the selected one must stay among them. */
            remote?: boolean
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
const [DefineOption, ReuseOption] = createReusableTemplate<{ option: Option<V> }>({
    props: { option: Object },
})
const labelOf = (value: unknown) => items.value.find(o => o.value === value)?.label ?? ''

// Reka mounts every option, which is slow with hundreds (a series' chapters). Virtualized, Reka
// leaves the filtering to us.
const VIRTUAL_MIN = 50
const OPTION_HEIGHT = 44
// Once the virtualizer has mounted, Reka's virtual mode stays on, so ours does too.
const virtual = ref(false)
watchEffect(() => (virtual.value ||= items.value.length > VIRTUAL_MIN))
const search = defineModel<string>('search', { default: '' })
// Typing into a closed combobox opens it, so the search only resets on close.
watch(open, isOpen => isOpen || (search.value = ''))
const { contains } = useFilter({ sensitivity: 'base' })
const filtered = computed(() =>
    props.remote || !search.value
        ? items.value
        : items.value.filter(o => contains(o.label, search.value))
)
// The virtualizer compares its options, which are Option objects, with the model value.
const valueOf = (v: unknown) => (typeof v === 'object' && v ? (v as Option<V>).value : v)
const sameValue = (a: unknown, b: unknown) => valueOf(a) === valueOf(b)
const controlAttrs = useControlAttrs()
</script>

<style>
@layer ui {
    /* Firefox otherwise sizes the root, a flex item at call sites, by the input's intrinsic width. */
    .a-combobox {
        min-width: 0;
    }

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

    .a-listbox [data-reka-virtualizer] > .a-option {
        width: 100%;
        height: 44px;

        & .a-option__text {
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
        }
    }

    .a-combobox__empty {
        padding: 12px 14px;
        color: var(--color-fg-muted);
        font-size: 14px;
    }
}
</style>
