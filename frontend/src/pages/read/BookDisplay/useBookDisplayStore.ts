import { contentApi } from '@/utils/api/content'
import type { BookChapter } from '@/utils/api/types'
import { useLocalStorage } from '@/utils/localStorage'
import { acceptHMRUpdate, defineStore } from 'pinia'
import { computed, markRaw, readonly, ref, toValue, type MaybeRefOrGetter } from 'vue'

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

export function getBookDisplaySettings(contentId: string) {
    const { value: settings } = useLocalStorage('reader:books', parseBookDisplaySettings)
    return {
        showHidden: computed(() => {
            return settings.value.showHidden.some(v => v.id === contentId)
        }),
        setShowHidden(show: boolean) {
            const index = settings.value.showHidden.findIndex(item => item.id === contentId)
            if (show && index === -1) {
                settings.value.showHidden.push({ id: contentId, dt: new Date().toISOString() })
            } else if (!show && index !== -1) {
                settings.value.showHidden.splice(index, 1)
            }
        },
    }
}

export function useBookDisplaySettings(contentId: MaybeRefOrGetter<string>) {
    return computed(() => getBookDisplaySettings(toValue(contentId)))
}

export function useVisibleBookChapters(
    contentId: MaybeRefOrGetter<string>,
    chapters: MaybeRefOrGetter<BookChapter[] | undefined>
) {
    const settings = useBookDisplaySettings(contentId)
    return computed(() => {
        let items = toValue(chapters) ?? []
        let len = items.length

        if (!settings.value.showHidden) {
            items = items.filter(ch => ch.linear)
        }

        return {
            items,
            hasHidden: len !== items.length,
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
