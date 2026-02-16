import { useLocalStorage } from '@/utils/localStorage'
import { acceptHMRUpdate, defineStore } from 'pinia'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'

const DEFAULT_ITEM_SIZE = 170

export interface GridSettings {
    itemSize: number
    hideItemCount: boolean
    itemCountMode: 'unread' | 'total'
    hideStatus: boolean
    hideTitle: boolean
}

const DEFAULTS: GridSettings = {
    itemSize: DEFAULT_ITEM_SIZE,
    hideItemCount: false,
    itemCountMode: 'unread',
    hideStatus: false,
    hideTitle: false,
}

export const useContentGridStore = defineStore('contentGrid', () => {
    const { value: entries } = useLocalStorage<Record<string, Partial<GridSettings>>>(
        'content-grid-settings',
        found => found ?? {}
    )

    function getForKey(key: MaybeRefOrGetter<string>) {
        return computed({
            get: (): GridSettings => ({ ...DEFAULTS, ...entries.value[toValue(key)] }),
            set: (v: Partial<GridSettings>) => {
                entries.value[toValue(key)] = { ...entries.value[toValue(key)], ...v }
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
