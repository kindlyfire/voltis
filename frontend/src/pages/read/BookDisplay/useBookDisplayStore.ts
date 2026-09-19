import { acceptHMRUpdate, defineStore } from 'pinia'
import { computed, markRaw, readonly, ref, toValue, type MaybeRefOrGetter } from 'vue'
import { contentApi } from '@/utils/api/content'
import type { BookChapter } from '@/utils/api/types'
import { useLocalStorage } from '@/utils/localStorage'

interface BookDisplaySettings {
    showHidden: Array<{
        id: string
        dt: string
    }>
}

function parseBookDisplaySettings(v: any): BookDisplaySettings {
    const defaults: BookDisplaySettings = { showHidden: [] }
    if (typeof v !== 'object' || v === null) return defaults
    const final = {
        showHidden: (Array.isArray(v.showHidden) ? (v.showHidden as any[]) : []).filter(item => {
            if (
                !(
                    typeof item === 'object' &&
                    item !== null &&
                    typeof item.id === 'string' &&
                    typeof item.dt === 'string'
                )
            )
                return false

            // If date is over a month old, remove it. Otherwise update it.
            const dt = new Date(item.dt)
            if (isNaN(dt.getTime())) return false
            const now = new Date()
            const diff = now.getTime() - dt.getTime()
            const oneMonth = 30 * 24 * 60 * 60 * 1000
            if (diff > oneMonth) return false

            item.dt = now.toISOString()
            return true
        }),
    }
    return final
}

export function useBookShowHidden(contentId: MaybeRefOrGetter<string>) {
    const store = useBookDisplayStore()
    return computed({
        get: () => store.settings.showHidden.some(v => v.id === toValue(contentId)),
        set(show) {
            const id = toValue(contentId)
            const on = !!show
            const index = store.settings.showHidden.findIndex(item => item.id === id)
            if (on && index === -1) {
                store.settings.showHidden.push({ id, dt: new Date().toISOString() })
            } else if (!on && index !== -1) {
                store.settings.showHidden.splice(index, 1)
            }
        },
    })
}

export function useVisibleBookChapters(
    contentId: MaybeRefOrGetter<string>,
    chapters: MaybeRefOrGetter<BookChapter[] | undefined>
) {
    const showHidden = useBookShowHidden(contentId)
    return computed(() => {
        const all = toValue(chapters) ?? []
        const linear = all.filter(ch => ch.linear)
        return {
            items: showHidden.value ? all : linear,
            hasHidden: linear.length !== all.length,
        }
    })
}

export const useBookDisplayStore = defineStore('book-display', () => {
    const { value: settings } = useLocalStorage('reader:books', parseBookDisplaySettings)
    const contentId = ref(null as string | null)
    const chapterHref = ref(null as string | null)

    const qContent = contentApi.useGet(() => contentId.value)
    const qChapters = contentApi.useBookChapters(() => contentId.value)
    const qChapterContent = contentApi.useBookChapter(() => contentId.value, chapterHref)

    return {
        settings,
        contentId: readonly(contentId),
        chapterHref: readonly(chapterHref),
        qChapters: markRaw(qChapters),
        chapters: qChapters.data,
        qContent: markRaw(qContent),
        content: qContent.data,
        qChapterContent: markRaw(qChapterContent),
        chapterContent: qChapterContent.data,
    }
})

if (import.meta.hot) {
    import.meta.hot.accept(acceptHMRUpdate(useBookDisplayStore, import.meta.hot))
}
