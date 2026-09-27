<template>
    <TabsRoot class="a-tabs" :model-value="modelValue ?? undefined" @update:model-value="select">
        <TabsList class="a-tabs__list" :aria-label="label">
            <TabsTrigger
                v-for="option in items"
                :key="option.value"
                :value="option.value"
                :disabled="option.disabled"
                class="a-tabs__trigger a-state"
            >
                <span class="a-tabs__label">{{ option.label }}</span>
            </TabsTrigger>
            <TabsIndicator class="a-tabs__indicator" />
        </TabsList>
        <!-- Hidden here rather than by Reka (`unmount-on-hide`): its Presence flips `hidden` a tick after
        the selection, too late for a consumer restoring the shown panel's scroll. -->
        <TabsContent
            v-for="option in items"
            :key="option.value"
            :ref="el => setPanel(option.value, el)"
            :value="option.value"
            force-mount
            :hidden="option.value !== modelValue"
            class="a-tabs__panel"
        >
            <slot :name="String(option.value)" />
        </TabsContent>
    </TabsRoot>
</template>

<script setup lang="ts" generic="V extends OptionValue">
import {
    TabsContent,
    TabsIndicator,
    TabsList,
    TabsRoot,
    TabsTrigger,
    type AcceptableValue,
} from 'reka-ui'
import { computed, type ComponentPublicInstance } from 'vue'
import { normalizeOptions, type Options, type OptionValue } from './options'

/**
 * Tabs with one named slot per option value. Every panel stays mounted (keeping its DOM and
 * state) and scrolls on its own, below a fixed tab bar: give the tabs a bounded height (a flex
 * column parent, like the drawer's body).
 */
const props = defineProps<{
    modelValue: V | null | undefined
    options: Options<V>
    /** Accessible name of the tab list. */
    label: string
}>()

const emit = defineEmits<{ 'update:modelValue': [value: V] }>()

defineSlots<Record<string, () => unknown>>()

const items = computed(() => normalizeOptions(props.options))

const panels = new Map<V, HTMLElement>()

function setPanel(value: V, el: Element | ComponentPublicInstance | null) {
    const node = el && '$el' in el ? el.$el : el
    if (node instanceof HTMLElement) panels.set(value, node)
    else panels.delete(value)
}

function select(value: AcceptableValue) {
    if (value != null) emit('update:modelValue', value as V)
}

defineExpose({
    /** A panel's scroll container (a hidden one reports `scrollTop` 0). */
    panel: (value: V) => panels.get(value),
})
</script>

<!-- Unscoped: the parts are Reka's elements. -->
<style>
@layer ui {
    .a-tabs {
        display: flex;
        flex: 1;
        flex-direction: column;
        min-height: 0;
    }

    .a-tabs__list {
        position: relative;
        display: flex;
        flex: none;
        border-bottom: 1px solid var(--color-outline-variant);
    }

    .a-tabs__trigger {
        display: inline-flex;
        flex: 1;
        align-items: center;
        justify-content: center;
        min-width: 0;
        height: 44px;
        padding: 0 16px;
        border: 0;
        background: transparent;
        color: var(--color-fg-muted);
        font-size: 14px;
        font-weight: 600;
        cursor: pointer;
        outline: none;

        &[data-state='active'] {
            color: var(--color-primary);
        }

        &:focus-visible {
            outline: 2px solid var(--color-primary);
            outline-offset: -3px;
        }

        &[data-disabled] {
            cursor: not-allowed;
            color: color-mix(in oklch, var(--color-fg) 38%, transparent);
        }
    }

    .a-tabs__label {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
    }

    /* Over the list's border. */
    .a-tabs__indicator {
        position: absolute;
        bottom: -1px;
        left: 0;
        width: var(--reka-tabs-indicator-size);
        height: 3px;
        border-radius: 3px 3px 0 0;
        background: var(--color-primary);
        transform: translateX(var(--reka-tabs-indicator-position));
        transition-property: width, transform;
        transition-duration: var(--duration-medium);
        transition-timing-function: var(--ease-standard);
        pointer-events: none;
    }

    .a-tabs__panel {
        flex: 1;
        min-height: 0;
        overflow-y: auto;
        overscroll-behavior: contain;
        outline: none;

        &:focus-visible {
            outline: 2px solid var(--color-primary);
            outline-offset: -2px;
        }
    }
}
</style>
