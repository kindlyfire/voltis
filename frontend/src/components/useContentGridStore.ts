import { useLocalStorage } from '@/utils/localStorage'
import { acceptHMRUpdate, defineStore } from 'pinia'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'

const DEFAULT_ITEM_SIZE = 170

interface GridSettings {
    itemSize: number
}

export const useContentGridStore = defineStore('contentGrid', () => {
    const { value: entries } = useLocalStorage<Record<string, GridSettings>>(
        'content-grid-settings',
        found => found ?? {}
    )

    function getForKey(key: MaybeRefOrGetter<string>) {
        return computed({
            get: () => entries.value[toValue(key)]?.itemSize ?? DEFAULT_ITEM_SIZE,
            set: (v: number) => {
                entries.value[toValue(key)] = { itemSize: v }
            },
        })
    }

    function resetKey(key: MaybeRefOrGetter<string>) {
        delete entries.value[toValue(key)]
    }

    return { getForKey, resetKey }
})

if (import.meta.hot) {
    import.meta.hot.accept(acceptHMRUpdate(useContentGridStore, import.meta.hot))
}
