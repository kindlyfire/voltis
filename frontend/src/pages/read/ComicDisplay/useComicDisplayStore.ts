import { keepPreviousData } from '@tanstack/vue-query'
import { useWindowSize } from '@vueuse/core'
import { acceptHMRUpdate, defineStore } from 'pinia'
import { ref, computed, shallowRef, type Ref } from 'vue'
import { useRouter } from 'vue-router'
import z from 'zod'
import { contentApi } from '@/utils/api/content'
import { useLocalStorage } from '@/utils/localStorage'
import { getLayoutTop } from '@/utils/misc'
import { useSiblings } from '../useSiblings'
import { createComicState, type ComicState } from './createComicState'
import { detectDirection } from './direction'
import { buildSpreads, sized, spreadOfPages } from './pagedLayout'
import type { PageDimensions, PagedScroller, ReaderMode, ReadingDirection } from './types'

const zSeriesSettings = z.object({
    mode: z.enum(['paged', 'longstrip']).nullable().catch(null),
    /** null = Auto */
    direction: z.enum(['ltr', 'rtl']).nullable().catch(null),
})
type SeriesSettings = z.infer<typeof zSeriesSettings>
const DEFAULT_SERIES_SETTINGS: SeriesSettings = { mode: null, direction: null }

/** Per field, so one stale or out-of-range value can't discard the rest. Object fallbacks are
 * functions: a `.catch({})` value would be one object shared by every parse. */
const zComicSettings = z.object({
    longstripWidth: z.number().min(10).max(100).catch(100),
    fit: z.enum(['screen', 'width', 'height']).catch('screen'),
    spread: z.enum(['single', 'double', 'auto']).catch('auto'),
    zoomWide: z.boolean().catch(true),
    /** RTL also mirrors arrow keys, click zones and swipes. */
    invertRtlControls: z.boolean().catch(true),
    seriesSettings: z.record(z.string(), zSeriesSettings).catch(() => ({})),
    /** Content ids of books whose spreads are shifted by one page. */
    shiftedBooks: z.record(z.string(), z.literal(true)).catch(() => ({})),
})

export interface ReaderContentOptions {
    contentId: string
    initialPage: number | 'last' | 'resume'
}

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
        const found = zComicSettings.safeParse(v)
        return found.success ? found.data : zComicSettings.parse({})
    })

    const state: Ref<ComicState | null> = ref(null)
    const content = computed(() => state.value?.content || null)

    // Mode keeps its `parent_id || ''` key for stored data; direction gives a parentless comic
    // its own entry.
    const modeKey = computed(() => content.value && (content.value.parent_id || ''))
    const directionKey = computed(
        () => content.value && (content.value.parent_id || content.value.id)
    )
    const entry = (key: string | null) =>
        (key !== null && settings.value.seriesSettings[key]) || DEFAULT_SERIES_SETTINGS
    const seriesSettings = computed<SeriesSettings>(() => ({
        mode: entry(modeKey.value).mode,
        direction: entry(directionKey.value).direction,
    }))
    /** Merges into a series entry, dropping it once every field is back at its default. */
    function updateSeriesSettings(key: string, patch: Partial<SeriesSettings>) {
        const next = { ...entry(key), ...patch }
        if (Object.values(next).every(v => v === null)) delete settings.value.seriesSettings[key]
        else settings.value.seriesSettings[key] = next
    }
    const mode = computed<ReaderMode>(
        () => seriesSettings.value.mode ?? detectMode(state.value?.pageDimensions || [])
    )

    // Placeholder data can be the previous series.
    const qParent = contentApi.useGet(() => content.value?.parent_id, {
        placeholderData: keepPreviousData,
    })
    const parent = computed(() => {
        const data = qParent.data.value
        return data && data.id === content.value?.parent_id ? data : null
    })
    const autoDirection = computed(() => detectDirection(content.value?.meta, parent.value?.meta))
    const direction = computed<ReadingDirection>(
        () => seriesSettings.value.direction ?? autoDirection.value
    )
    /** Arrow keys, click zones and swipes are mirrored. */
    const controlsFlipped = computed(
        () =>
            mode.value === 'paged' && direction.value === 'rtl' && settings.value.invertRtlControls
    )

    const shifted = computed(
        () => !!content.value && !!settings.value.shiftedBooks[content.value.id]
    )
    function toggleShift() {
        const id = content.value?.id
        if (!id) return
        if (settings.value.shiftedBooks[id]) delete settings.value.shiftedBooks[id]
        else settings.value.shiftedBooks[id] = true
    }

    const windowSize = useWindowSize()
    const spreadDouble = computed(
        () =>
            settings.value.spread === 'double' ||
            (settings.value.spread === 'auto' && windowSize.width.value > windowSize.height.value)
    )
    // Dimensions arrive before the loaders, so nothing is laid out until loading ends.
    const spreads = computed(() => {
        const s = state.value
        if (!s || s.loading) return []
        return buildSpreads(s.pageDimensions, { double: spreadDouble.value, shift: shifted.value })
    })
    const spreadOfPage = computed(() =>
        spreadOfPages(spreads.value, state.value?.pageDimensions.length ?? 0)
    )
    /** Undefined while loading or without pages. */
    const spreadIndex = computed<number | undefined>(() =>
        state.value ? spreadOfPage.value[state.value.page] : undefined
    )
    const currentSpread = computed(() =>
        spreadIndex.value === undefined ? undefined : spreads.value[spreadIndex.value]
    )

    /** Registered by the paged view while mounted. */
    const pagedScroller = shallowRef<PagedScroller | null>(null)

    // A longstrip scroll of our own (a placement) is under way.
    const restoring = ref(false)
    // On-page input since the last placement: only then is a longstrip scroll reading.
    const armed = ref(false)
    // Paged: past the last page with no next sibling, on the end card.
    const atEnd = ref(false)
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
        if (modeKey.value === null) return
        placement()
        updateSeriesSettings(modeKey.value, { mode })
    }

    function setDirection(direction: ReadingDirection | null) {
        if (directionKey.value === null) return
        updateSeriesSettings(directionKey.value, { direction })
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
        setDirection,
        shifted,
        toggleShift,

        // Paged layout
        autoDirection,
        direction,
        controlsFlipped,
        spreadDouble,
        spreads,
        spreadIndex,
        currentSpread,
        pagedScroller,

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
