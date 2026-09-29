import { useDebounceFn } from '@vueuse/core'
import { reactive, readonly, toRefs } from 'vue'
import {
    bumpRecentlyRead,
    contentApi,
    invalidateRecentlyRead,
    invalidateStatusChange,
} from '@/utils/api/content'
import type { Content, ReadingStatus, UserToContent } from '@/utils/api/types'
import { API_URL } from '@/utils/fetch'
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
    } | null
    loaders: PageLoaderState[]
    pageDimensions: PageDimensions[]
}

const PAGE_CACHE_WINDOW = 8
const PRELOAD_COUNT = 8
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

    let userData: UserToContent | null = null
    let updateProgressPromise = Promise.resolve()
    let disposed = false
    // Opening or restoring a position isn't reading: only a page change by the user sets a status.
    let navigated = false
    const contentController = new AbortController()

    const updateProgress = useDebounceFn(() => {
        if (!state.content) return
        // Before the write: its response can arrive after the home page has mounted.
        bumpRecentlyRead(state.content)

        updateProgressPromise = updateProgressPromise
            .then(async () => {
                const pages = state.pageDimensions.length
                const status: ReadingStatus | undefined =
                    navigated && (!userData?.status || userData.status === 'reading')
                        ? state.page === pages - 1
                            ? 'completed'
                            : 'reading'
                        : undefined

                const previous = userData?.status ?? null
                userData = await contentApi.updateUserData(state.content!.id, {
                    status,
                    progress: {
                        ...userData?.progress,
                        current_page: state.page,
                        ...(pages > 0 && {
                            progress_percent: Math.round(((state.page + 1) / pages) * 1000) / 10,
                        }),
                    },
                })
                if (status && status !== previous) invalidateStatusChange(state.content!.parent_id)
                else invalidateRecentlyRead()
            })
            .catch(err => {
                console.error('Failed to update reading progress', err)
            })
    }, 1000)

    contentApi
        .get(contentId, { signal: contentController.signal })
        .then(content => {
            if (disposed) return
            if (!state.handlers) {
                throw new Error('Comic handlers not set')
            }

            state.content = content
            userData = content.user_data ?? null
            state.pageDimensions = (content.file_data.pages ?? []).map(p => ({
                width: p[1],
                height: p[2],
            }))
            // We just use `reactive` to turn it into UnwrapNestedRefs<_>
            state.loaders = reactive(
                state.pageDimensions.map((_, index) => createPageLoader(index, getPageUrl(index)))
            )
            state.error = null
            state.loading = false
            let initialPage = 0
            if (state.initialPage === 'resume') {
                const progress = userData?.progress?.current_page ?? 0
                if (typeof progress === 'number' && !isNaN(progress)) {
                    initialPage = progress
                }
            } else if (state.initialPage === 'last') {
                initialPage = state.pageDimensions.length - 1
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

    /** `restore` places the reader without counting as navigation. */
    function setPage(page: number, { restore = false } = {}) {
        if (!restore) navigated = true
        state.page = Math.min(Math.max(0, page), state.pageDimensions.length - 1)
        cleanupDistantLoaders()
        preloadPages()
        updateProgress()
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

    return reactive({
        ...toRefs(readonly(state)),
        setPage,
        setHandlers(handlers: ComicStateValues['handlers']) {
            state.handlers = handlers
        },
        /** Bumps and starts the exit write; the returned promise settles when it lands. */
        leave() {
            updateProgress.flush()
            return updateProgressPromise
        },
        dispose() {
            disposed = true
            contentController.abort()
            updateProgress.flush()
            for (const loader of state.loaders) {
                loader.dispose()
            }
            return updateProgressPromise
        },
    })
}

export type ComicState = ReturnType<typeof createComicState>
