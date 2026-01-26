import { QueryClient } from '@tanstack/vue-query'
import {
    computed,
    onBeforeMount,
    onMounted,
    onUnmounted,
    ref,
    toValue,
    type MaybeRefOrGetter,
    type Ref,
} from 'vue'

export const queryClient = new QueryClient({})

export const LIBRARY_GRID_CLASSES =
    'grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-6 3xl:grid-cols-8 gap-4'

export function getLayoutTop() {
    try {
        return parseInt(
            getComputedStyle(document.getElementsByClassName('v-main')[0]!).getPropertyValue(
                '--v-layout-top'
            ) || '0'
        )
    } catch {
        return 0
    }
}

/** Like `Array.at` but without looking backwards for negative indexes */
export function arrayAtNowrap<T>(arr: T[], index: number): T | undefined {
    if (index < 0 || index >= arr.length) return undefined
    return arr[index]
}

/**
 * Utility to create a value that can be overridden by multiple layers, with
 * priority given to the highest layer that has an override set.  When no layers
 * have an override, the initial value is used.
 */
export function createOverridableValue<TValue, const TLayer extends string>(
    initial: MaybeRefOrGetter<TValue>,
    layers: TLayer[]
) {
    const layerIndex = (layer: TLayer) => {
        const index = layers.indexOf(layer)
        if (index === -1) {
            throw new Error(`Layer ${layer} not found in overridable value`)
        }
        return index
    }

    const obj: {
        initialValue: Ref<TValue>
        overrides: Ref<(TValue | undefined)[]>
        value: Ref<TValue>
        useLayer: (layer: TLayer, value?: TValue) => (v: TValue | undefined) => void
        setLayer: (layer: TLayer, value: TValue | undefined) => void
    } = {
        initialValue: computed(() => toValue(initial)),
        overrides: ref([]),
        value: computed(() => {
            for (let i = layers.length - 1; i >= 0; i--) {
                const override = obj.overrides.value[i]
                if (override != null) {
                    return override
                }
            }
            return obj.initialValue.value
        }),
        useLayer(layer: TLayer, value?: TValue) {
            onMounted(() => {
                if (typeof value !== 'undefined') {
                    obj.setLayer(layer, value)
                }
            })
            onUnmounted(() => {
                obj.setLayer(layer, undefined)
            })
            return (v: TValue | undefined) => {
                obj.setLayer(layer, v)
            }
        },
        setLayer(layer: TLayer, value: TValue | undefined) {
            const index = layerIndex(layer)
            this.overrides.value[index] = value
        },
    }

    return obj
}
