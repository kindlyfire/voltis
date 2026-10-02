import { acceptHMRUpdate, defineStore } from 'pinia'
import { ref, computed, type Ref } from 'vue'
import { useRouter } from 'vue-router'
import z from 'zod'
import { useLocalStorage } from '@/utils/localStorage'
import { getLayoutTop } from '@/utils/misc'
import { useSiblings } from '../useSiblings'
import { createComicState, type ComicState } from './createComicState'
import type { PageDimensions, ReaderMode } from './types'

const zComicSettings = z.object({
    longstripWidth: z.number().min(10).max(100).default(100),
    seriesSettings: z
        .record(
            z.string(),
            z.object({
                mode: z.enum(['paged', 'longstrip']).nullable().default(null),
            })
        )
        .default({}),
})

export interface ReaderContentOptions {
    contentId: string
    initialPage: number | 'last' | 'resume'
}

// A width or height of 0 means the size is unknown.
const sized = (p: PageDimensions) => p.width > 0 && p.height > 0

/** Detects longstrips by the average aspect ratio of the pages with a known size. */
export function detectMode(pages: readonly PageDimensions[]): ReaderMode {
    const known = pages.filter(sized)
    if (known.length === 0) return 'paged'
    const avgAspectRatio = known.reduce((sum, p) => sum + p.height / p.width, 0) / known.length
    return avgAspectRatio > 1.6 ? 'longstrip' : 'paged'
}

export function pageStyle(page: PageDimensions, widthPercent: number) {
    if (!sized(page)) return {}
    return {
        width: `min(${widthPercent}%, ${page.width}px)`,
        aspectRatio: `${page.width} / ${page.height}`,
    }
}

export const useReaderStore = defineStore('reader', () => {
    const router = useRouter()

    const sidebarOpen = ref(false)
    const { value: settings } = useLocalStorage('reader:comics', v => {
        try {
            return zComicSettings.parse(v)
        } catch {
            return zComicSettings.parse({})
        }
    })
    const seriesSettings = computed(() => {
        const c = state.value?.content
        if (!c)
            return {
                mode: null,
            }
        const s = settings.value.seriesSettings[c.parent_id || ''] || {
            mode: null,
        }
        return s
    })
    const mode = computed<ReaderMode>(() => {
        const s = seriesSettings.value
        if (!s) return 'paged'
        if (s.mode) return s.mode
        return detectMode(state.value?.pageDimensions || [])
    })

    const state: Ref<ComicState | null> = ref(null)
    // A longstrip scroll of our own (a placement) is under way.
    const restoring = ref(false)
    // On-page input since the last placement: only then is a longstrip scroll reading.
    const armed = ref(false)
    // Paged: past the last page with no next sibling, on the end card.
    const atEnd = ref(false)
    const content = computed(() => state.value?.content || null)
    const sync = computed(() => state.value?.sync ?? null)

    const siblings = useSiblings(content)
    /** Past the last page: on to the next sibling, or the end card, which waits for the siblings
     * (or offers their Retry) and leaves the next press to go on. */
    function goPastEnd() {
        const next = siblings.value.next
        if (next) return goToSibling('next')
        atEnd.value = true
    }

    const progress = computed(() => {
        const pagesVal = state.value?.pageDimensions
        if (!pagesVal?.length) return 0
        return ((state.value?.page ?? 0) / pagesVal.length) * 100
    })

    function leave() {
        state.value?.leave()
    }

    function reset() {
        endPlacement?.()
        restoring.value = true
        armed.value = false
        atEnd.value = false
    }

    function dispose() {
        sidebarOpen.value = false
        reset()
        state.value?.dispose()
        state.value = null
    }

    function setMode(mode: ReaderMode | null) {
        const c = state.value?.content
        if (!c) return
        placement()

        if (mode == null) {
            if (settings.value.seriesSettings[c.parent_id || '']) {
                delete settings.value.seriesSettings[c.parent_id || '']
            }
            return
        }

        const s = settings.value.seriesSettings[c.parent_id || ''] || {
            mode: null,
        }
        s.mode = mode
        settings.value.seriesSettings[c.parent_id || ''] = s
    }

    function setContent(options: ReaderContentOptions) {
        if (options.contentId === state.value?.contentId) {
            return
        }
        // The sibling's own load queues behind this exit write.
        state.value?.dispose()
        const s = createComicState(options.contentId, options.initialPage)
        state.value = s
        reset()
        s.setHandlers({
            onPlace: page => {
                if (state.value === s) goToPage(page, 'instant')
            },
            onReady: () => {
                if (state.value !== s) return

                if (mode.value === 'longstrip') {
                    requestAnimationFrame(() => {
                        if (state.value === s) goToPage(s.page, 'instant')
                    })
                }

                // This is to replace "last" or "resume" in the URL with the
                // actual page number
                const page = s.page
                router.replace({ query: page === 0 ? {} : { page: page + 1 } })
            },
        })
    }

    function setPage(page: number, options?: { restore?: boolean }) {
        if (page === state.value?.page) {
            return
        }
        router.replace({ query: page === 0 ? {} : { page: page + 1 } })
        state.value?.setPage(page, options)
    }

    /** Every move the reader didn't make: earlier reading goes out first, sealed, and the
     * longstrip's scrolling is reading again only after new input. */
    function placement() {
        state.value?.sync.flush()
        armed.value = false
    }

    /** A placement (slider, restore, layout change), which writes nothing. */
    function goToPage(page: number | null = null, behavior: ScrollBehavior = 'instant') {
        if (page === null) {
            page = state.value?.page ?? 0
        }
        placement()
        atEnd.value = false
        setPage(page, { restore: true })
        if (mode.value === 'longstrip') {
            const pageEl = document.getElementById(`longstrip-page-${page}`)
            if (pageEl) placeScroll(pageEl.offsetTop - getLayoutTop(), behavior)
        }
    }

    /** Scrolls the longstrip as a placement, until the scroll ends. */
    let endPlacement: (() => void) | null = null
    function placeScroll(top: number, behavior: ScrollBehavior) {
        endPlacement?.()
        restoring.value = true
        armed.value = false
        const max = document.documentElement.scrollHeight - window.innerHeight
        const moves = Math.round(Math.min(Math.max(top, 0), max)) !== Math.round(window.scrollY)
        window.scrollTo({ top, behavior })
        if (!moves) return void requestAnimationFrame(() => (restoring.value = false))
        // Safari < 26.2 has no scrollend: the scroll has ended once it pauses.
        const hasScrollEnd = 'onscrollend' in window
        let timer: ReturnType<typeof setTimeout> | undefined
        const onScroll = () => {
            clearTimeout(timer)
            timer = setTimeout(done, 150)
        }
        const done = () => {
            clearTimeout(timer)
            window.removeEventListener('scrollend', done)
            window.removeEventListener('scroll', onScroll)
            endPlacement = null
            restoring.value = false
        }
        endPlacement = done
        if (hasScrollEnd) window.addEventListener('scrollend', done, { once: true })
        else {
            window.addEventListener('scroll', onScroll, { passive: true })
            onScroll()
        }
    }

    function goToSibling(target: (string & {}) | 'next' | 'prev') {
        let id: string | undefined = target
        if (target === 'next' || target === 'prev') {
            const { status, prev, next } = siblings.value
            if (status !== 'ready') return
            id = (target === 'next' ? next : prev)?.id
        }
        if (id) router.push({ name: 'read-content', params: { id }, query: { page: 'resume' } })
    }

    return {
        // Persistent state
        sidebarOpen,
        settings,
        seriesSettings,
        mode,
        setMode,

        // Content state (readonly)
        state,
        siblings,
        progress,
        restoring,
        armed,
        atEnd,
        sync,

        // Actions
        setContent,
        goToPage,
        placement,
        goToSibling,
        goPastEnd,
        leave,
        dispose,
        setPage,
    }
})

export type ReaderStore = ReturnType<typeof useReaderStore>

if (import.meta.hot) {
    import.meta.hot.accept(acceptHMRUpdate(useReaderStore, import.meta.hot))
}
