import { QueryClient } from '@tanstack/vue-query'
import { useEventListener } from '@vueuse/core'
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
import { useRouter } from 'vue-router'

export const queryClient = new QueryClient({})

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
        getLayer: (layer: TLayer) => TValue | undefined
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
        getLayer(layer: TLayer): TValue | undefined {
            const index = layerIndex(layer)
            return this.overrides.value[index]
        },
    }

    return obj
}

/**
 * Returns pointer event handlers that fire `fn` immediately on press,
 * then repeatedly after an initial delay.
 */
export function useRepeatOnHold(fn: () => void, { initialDelay = 400, interval = 100 } = {}) {
    let timer: any = null
    function stop() {
        if (timer != null) {
            clearTimeout(timer)
            timer = null
        }
    }
    function start() {
        stop()
        fn()
        const schedule = (delay: number) => {
            timer = setTimeout(() => {
                fn()
                schedule(interval)
            }, delay)
        }
        schedule(initialDelay)
    }
    onUnmounted(stop)
    return { onPointerdown: start, onPointerup: stop, onPointerleave: stop }
}

export function useSystemTheme() {
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    const isDark = ref(mediaQuery.matches)
    useEventListener(mediaQuery, 'change', (e: MediaQueryListEvent) => {
        isDark.value = e.matches
    })
    return isDark
}

export function jsonClone<T>(obj: T): T {
    return JSON.parse(JSON.stringify(obj))
}

/**
 * Two-way sync multiple query params as plain values (not JSON). Returns an object of refs, one per
 * key specified in defaults. Empty string values are omitted from the URL.
 */
export function useRouteQueryParams<T extends Record<string, string | null>>(
    defaults: T
): { [K in keyof T]: Ref<T[K]> } {
    const router = useRouter()
    const result = {} as { [K in keyof T]: Ref<T[K]> }

    // Pending changes to batch multiple updates in the same tick. Necessary because
    // router.currentRoute.value.query doesn't update immediately after router.replace, so doing two
    // or more updates in the same tick will override previous ones.
    let pending: Record<string, string | null> | null = null
    function flushPending() {
        if (!pending) return
        const changes = pending
        pending = null
        const newQuery = { ...router.currentRoute.value.query }
        for (const [k, v] of Object.entries(changes)) {
            if (v === null) {
                delete newQuery[k]
            } else {
                newQuery[k] = v
            }
        }
        router.replace({ query: newQuery })
    }

    for (const key of Object.keys(defaults) as (keyof T)[]) {
        result[key] = computed({
            get: () => {
                const val = router.currentRoute.value.query[key as string]
                return (val as T[keyof T]) ?? defaults[key]
            },
            set: (value: T[keyof T]) => {
                const shouldDelete = value === '' || value === defaults[key]
                if (!pending) {
                    pending = {}
                    queueMicrotask(flushPending)
                }
                pending[key as string] = shouldDelete ? null : (value as string)
            },
        })
    }

    return result
}
