import { ref, watch, type Ref } from 'vue'

export function useLocalStorage<T>(
    key: string,
    defaultFn: (foundValue: any) => T
): {
    value: Ref<T>
    clear: () => void
} {
    let value: Ref<T>
    try {
        const stored = localStorage.getItem(key)
        const parsed = stored !== null ? JSON.parse(stored) : undefined
        value = ref(defaultFn(parsed)) as Ref<T>
    } catch (e) {
        console.error(`Error parsing localStorage item for key "${key}":`, e)
        value = ref(defaultFn(undefined)) as Ref<T>
    }

    watch(
        value,
        newValue => {
            localStorage.setItem(key, JSON.stringify(newValue))
        },
        { deep: true }
    )

    function clear() {
        localStorage.removeItem(key)
        value.value = defaultFn(undefined)
    }

    return { value, clear }
}
