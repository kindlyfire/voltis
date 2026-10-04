import { reactive, readonly, toRefs } from 'vue'
import { contentApi } from '@/utils/api/content'
import type { Content, ReadingProgress } from '@/utils/api/types'
import { API_URL } from '@/utils/fetch'
import { attachReading } from '../readingSync'
import type { PageDimensions } from './types'
import { createPageLoader, getPagesInPreloadOrder, type PageLoaderState } from './usePageLoader'

export interface ComicStateValues {
    initialPage: number | 'last' | 'resume'
    page: number
    contentId: string
    content: Content | null
    error: string | null
    loading: boolean
    handlers: {
        onReady: () => void
        /** Places the reader on a page, writing nothing. */
        onPlace: (page: number) => void
    } | null
    loaders: PageLoaderState[]
    pageDimensions: PageDimensions[]
}

const PAGE_CACHE_WINDOW = 10
const PRELOAD_COUNT = 10
const PRELOAD_CONCURRENCY = 3

export function createComicState(contentId: string, initialPage: number | 'last' | 'resume') {
    const state = reactive<ComicStateValues>({
        initialPage,
        page: 0,
        contentId,
        content: null,
        error: null,
        loading: true,
        handlers: null,
        loaders: [],
        pageDimensions: [],
    })

    let disposed = false
    const contentController = new AbortController()

    const pages = () => state.pageDimensions.length
    const clamp = (page: number) => Math.min(Math.max(0, page), pages() - 1)

    function pageFor(progress: ReadingProgress): number {
        if (progress.at_end) return Math.max(0, pages() - 1)
        const page = progress.current_page
        return typeof page === 'number' && !isNaN(page) ? clamp(page) : 0
    }

    function position(page: number): ReadingProgress {
        return {
            current_page: page,
            // Only a finish reaches 100%.
            ...(pages() > 0 && {
                progress_percent: Math.min(Math.round((page / pages()) * 1000) / 10, 99.9),
            }),
        }
    }

    const samePage = (a: ReadingProgress, b: ReadingProgress) => pageFor(a) === pageFor(b)

    const sync = attachReading(contentId, {
        content: () => state.content,
        restore: progress => state.handlers?.onPlace(pageFor(progress)),
        describe: progress => `p.${pageFor(progress) + 1}`,
        samePosition: samePage,
        samePlace: samePage,
    })

    /** Reads the content and its saved state, then places the reader; again after a failure. */
    function open() {
        state.error = null
        state.loading = true
        contentApi
            .get(contentId, { signal: contentController.signal }, { pageSizes: true })
            .then(async content => {
                if (disposed) return
                if (!state.handlers) {
                    throw new Error('Comic handlers not set')
                }

                state.content = content
                state.pageDimensions = (content.file_data.pages ?? []).map(p => ({
                    width: p[1] ?? 0,
                    height: p[2] ?? 0,
                }))
                // Positions compare by page, so the saved state is read, and reconciled, only now.
                const saved = await sync.load()
                if (disposed) return
                // We just use `reactive` to turn it into UnwrapNestedRefs<_>
                state.loaders = reactive(
                    state.pageDimensions.map((_, index) =>
                        createPageLoader(index, getPageUrl(index))
                    )
                )
                state.error = null
                state.loading = false
                let initialPage = 0
                if (state.initialPage === 'resume') {
                    initialPage = pageFor(saved)
                } else if (state.initialPage === 'last') {
                    initialPage = pages() - 1
                } else {
                    initialPage = state.initialPage
                }
                setPage(initialPage, { restore: true })
                state.handlers.onReady()
            })
            .catch(e => {
                if (disposed) return
                console.error(e)
                state.error = e instanceof Error ? e.message : String(e)
                state.loading = false
            })
    }
    open()

    /** Real reading unless `restore`, which places the reader without writing. */
    function setPage(page: number, { restore = false } = {}) {
        state.page = clamp(page)
        cleanupDistantLoaders()
        preloadPages()
        if (restore) sync.placed(position(state.page))
        else sync.moved(position(state.page))
    }

    function finish() {
        if (!state.content) return
        sync.finish({ current_page: pages() - 1, progress_percent: 100, at_end: true })
    }

    function cleanupDistantLoaders() {
        for (const loader of state.loaders) {
            if (Math.abs(loader.index - state.page) > PAGE_CACHE_WINDOW) {
                loader.dispose()
            }
        }
    }

    function preloadPages() {
        const order = getPagesInPreloadOrder(state.pageDimensions.length, state.page)
        let loading = 0
        for (const index of order.slice(0, PRELOAD_COUNT)) {
            const loader = state.loaders[index]
            if (!loader) continue
            if (loader.blobUrl) continue
            if (loading >= PRELOAD_CONCURRENCY) break
            loader.load()
            loading++
        }
    }

    function getPageUrl(index: number): string {
        return `${API_URL}/files/comic-page/${state.contentId}/${index}?v=${state.content?.file_mtime ?? ''}`
    }

    function onVisibility() {
        if (document.visibilityState === 'hidden') sync.hide()
        else sync.check()
    }
    const onPageHide = () => sync.hide()
    const onFocus = () => sync.check()
    document.addEventListener('visibilitychange', onVisibility)
    window.addEventListener('pagehide', onPageHide)
    window.addEventListener('focus', onFocus)

    return reactive({
        ...toRefs(readonly(state)),
        sync,
        setPage,
        finish,
        retry() {
            if (state.error) open()
        },
        setHandlers(handlers: ComicStateValues['handlers']) {
            state.handlers = handlers
        },
        /** Sends the exit write before the next page reads. */
        leave() {
            sync.flush()
        },
        dispose() {
            disposed = true
            contentController.abort()
            document.removeEventListener('visibilitychange', onVisibility)
            window.removeEventListener('pagehide', onPageHide)
            window.removeEventListener('focus', onFocus)
            for (const loader of state.loaders) {
                loader.dispose()
            }
            sync.detach()
        },
    })
}

export type ComicState = ReturnType<typeof createComicState>
