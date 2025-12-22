import { ref, shallowRef, computed, watch, toRaw, readonly } from 'vue'
import { acceptHMRUpdate, defineStore } from 'pinia'
import { useRouter } from 'vue-router'
import type { PageInfo, ReaderMode, SiblingsInfo, SiblingContent } from './types'
import { createPageLoader, getPagesInPreloadOrder, type PageLoaderState } from './usePageLoader'
import { contentApi } from '@/utils/api/content'
import { keepPreviousData } from '@tanstack/vue-query'
import { useLocalStorage } from '@/utils/localStorage'

interface ComicSettings {
	longstripWidth: number
	mode: ReaderMode
}

function parseComicSettings(v: unknown): ComicSettings {
	const defaults: ComicSettings = { longstripWidth: 100, mode: 'paged' }
	if (typeof v !== 'object' || v === null) return defaults
	const obj = v as Record<string, unknown>
	return {
		mode: obj.mode === 'paged' || obj.mode === 'longstrip' ? obj.mode : defaults.mode,
		longstripWidth:
			typeof obj.longstripWidth === 'number' &&
			obj.longstripWidth >= 10 &&
			obj.longstripWidth <= 100
				? obj.longstripWidth
				: defaults.longstripWidth,
	}
}

/**
 * There's a problem in longstrip mode, where we use the currentPage to scroll,
 * but we also use the scroll position to set the currentPage. To prevent loop,
 * we differentiate between the two.
 */
export const SetPage = {
	// Initial, will scroll instantly (not smooth)
	INITIAL: 'initial',
	// Will scroll to the page in longstrip mode
	FOREGROUND: 'active',
	// Will not scroll in longstrip mode
	BACKGROUND: 'passive',
} as const
export type SetPage = (typeof SetPage)[keyof typeof SetPage]

const PAGE_CACHE_WINDOW = 5
const PRELOAD_COUNT = 8
const PRELOAD_CONCURRENCY = 3

export interface ReaderContentOptions {
	contentId: string
	getPageImageUrl: (index: number) => string
	onReachStart?: () => void
	onReachEnd?: () => void
	onGoToSibling?: (id: string, fromEnd?: boolean) => void
}

export const useReaderStore = defineStore('reader', () => {
	const router = useRouter()

	const sidebarOpen = ref(false)
	const { value: settings } = useLocalStorage('reader:comics', parseComicSettings)
	const contentId = ref<string>('')
	const pages = shallowRef<PageInfo[]>([])
	const getPageUrlFn = shallowRef<(index: number) => string>(() => '')
	const onReachStartFn = shallowRef<(() => void) | undefined>()
	const onReachEndFn = shallowRef<(() => void) | undefined>()
	const onScrollToPageFn = shallowRef<((index: number, instant: boolean) => void) | undefined>()
	const onGoToSiblingFn = shallowRef<((id: string, fromEnd?: boolean) => void) | undefined>()

	const loaders = shallowRef<PageLoaderState[]>([])
	const abortController = shallowRef<AbortController | null>(null)
	const scrollRef = ref<HTMLElement | null>(null)

	const qContent = contentApi.useGet(contentId)
	const content = qContent.data
	const qSiblings = contentApi.useList(
		() => {
			if (content.value?.parent_id) return { parent_id: content.value.parent_id }
		},
		{
			placeholderData: keepPreviousData,
		}
	)
	const siblings = computed<SiblingsInfo | null>(() => {
		const siblings = qSiblings.data.value
		if ((content.value && !content.value?.parent_id) || !siblings) return null
		const items = [...siblings]
			.sort((a, b) => (a.order ?? 0) - (b.order ?? 0))
			.map(c => ({ id: c.id, title: c.title, order: c.order }))
		const currentIndex = items.findIndex(item => item.id === content.value?.id)
		return {
			items,
			currentIndex: currentIndex >= 0 ? currentIndex : 0,
		}
	})

	watch(
		() => content.value,
		newContent => {
			if (newContent) {
				pages.value = (newContent.meta.pages ?? []).map(p => ({
					width: p[1],
					height: p[2],
				}))
				_initializeLoaders()
				_preloadPages()
			}
		},
		{ immediate: true }
	)

	const currentPage = ref(0)
	watch(
		() => [router.currentRoute.value.query.page, pages.value, onScrollToPageFn.value] as const,
		([page, pages, onScrollToPageFn]) => {
			if (!pages.length) return
			if (settings.value.mode === 'longstrip' && !onScrollToPageFn) return

			let newPage: number
			if (page === 'last') {
				newPage = Math.max(0, pages.length - 1)
			} else if (page === '') {
				newPage = 0
			} else {
				const parsed = typeof page === 'string' ? parseInt(page, 10) : NaN
				if (isNaN(parsed) || parsed < 0 || parsed >= pages.length) {
					newPage = _setPageParam(0)
				} else {
					newPage = parsed
				}
			}
			if (newPage !== currentPage.value) {
				setCurrentPage(newPage, SetPage.INITIAL)
			}
			_cleanupDistantLoaders()
			_preloadPages()
		},
		{
			immediate: true,
		}
	)

	const progress = computed(() => {
		const pagesVal = pages.value
		if (pagesVal.length === 0) return 0
		return ((currentPage.value + 1) / pagesVal.length) * 100
	})

	const prevSibling = computed<SiblingContent | null>(() => {
		const s = siblings.value
		if (!s || s.currentIndex <= 0) return null
		return s.items[s.currentIndex - 1] ?? null
	})

	const nextSibling = computed<SiblingContent | null>(() => {
		const s = siblings.value
		if (!s || s.currentIndex >= s.items.length - 1) return null
		return s.items[s.currentIndex + 1] ?? null
	})

	function _setPageParam(page: number) {
		router.replace({ query: page === 0 ? {} : { page } })
		return page
	}

	function _initializeLoaders() {
		const pagesVal = pages.value
		const signal = abortController.value?.signal
		if (!signal) return

		loaders.value = pagesVal.map((_, index) =>
			createPageLoader(index, getPageUrlFn.value(index), signal)
		)
	}

	function _cleanupDistantLoaders() {
		for (const loader of loaders.value) {
			if (Math.abs(loader.index - currentPage.value) > PAGE_CACHE_WINDOW) {
				loader.dispose()
			}
		}
	}

	function _preloadPages() {
		const pagesVal = pages.value
		const order = getPagesInPreloadOrder(pagesVal.length, currentPage.value)
		let loading = 0
		for (const index of order.slice(0, PRELOAD_COUNT)) {
			const loader = loaders.value[index]
			if (!loader) continue
			if (loader.blobUrl.value) continue
			if (loading >= PRELOAD_CONCURRENCY) break
			loader.load()
			loading++
		}
	}

	function _isAtBottom(): boolean {
		const el = scrollRef.value
		if (!el) return true
		return el.scrollTop + el.clientHeight >= el.scrollHeight - 10
	}

	function _isAtTop(): boolean {
		const el = scrollRef.value
		if (!el) return true
		return el.scrollTop <= 10
	}

	function _scrollByViewport(factor: number) {
		const el = scrollRef.value
		if (!el) return
		el.scrollBy({ top: el.clientHeight * factor, behavior: 'smooth' })
	}

	function setContent(options: ReaderContentOptions) {
		// Cleanup previous content
		disposeLoaders()

		// Set new content
		contentId.value = options.contentId
		getPageUrlFn.value = options.getPageImageUrl
		onReachStartFn.value = options.onReachStart
		onReachEndFn.value = options.onReachEnd
		onGoToSiblingFn.value = options.onGoToSibling
		abortController.value = new AbortController()
	}

	function setCurrentPage(page: number, mode: SetPage) {
		const pagesVal = pages.value
		const clamped = Math.min(Math.max(0, page), pagesVal.length - 1)
		_setPageParam(clamped)
		currentPage.value = clamped
		if (mode === SetPage.FOREGROUND || mode === SetPage.INITIAL) {
			onScrollToPageFn.value?.(clamped, mode === SetPage.INITIAL)
		}
	}

	function disposeLoaders() {
		abortController.value?.abort()
		for (const loader of loaders.value) {
			loader.dispose()
		}
		loaders.value = []
	}

	function goToPage(page: number) {
		setCurrentPage(page, SetPage.FOREGROUND)
	}

	function handleNext() {
		const pagesVal = pages.value
		if (settings.value.mode === 'paged') {
			if (currentPage.value >= pagesVal.length - 1) {
				onReachEndFn.value?.()
			} else {
				setCurrentPage(currentPage.value + 1, SetPage.FOREGROUND)
			}
		} else {
			if (_isAtBottom()) {
				onReachEndFn.value?.()
			} else {
				_scrollByViewport(0.9)
			}
		}
	}

	function handlePrev() {
		if (settings.value.mode === 'paged') {
			if (currentPage.value <= 0) {
				onReachStartFn.value?.()
			} else {
				setCurrentPage(currentPage.value - 1, SetPage.FOREGROUND)
			}
		} else {
			if (_isAtTop()) {
				onReachStartFn.value?.()
			} else {
				_scrollByViewport(-0.9)
			}
		}
	}

	function getLoader(index: number): PageLoaderState | undefined {
		return loaders.value[index]
	}

	function goToSibling(id: string, fromEnd = false) {
		onGoToSiblingFn.value?.(id, fromEnd)
	}

	function setOnScrollToPageFn(fn: undefined | ((index: number, instant: boolean) => void)) {
		onScrollToPageFn.value = fn
	}

	return {
		// Persistent state
		sidebarOpen,
		settings,

		// Content state (readonly)
		contentId: readonly(contentId),
		content,
		pages,
		siblings,
		prevSibling,
		nextSibling,

		// Derived state
		currentPage,
		progress,
		loaders,
		scrollRef,

		// Actions
		setContent,
		setOnScrollToPageFn,
		setCurrentPage,
		disposeLoaders,
		goToPage,
		handleNext,
		handlePrev,
		getLoader,
		goToSibling,
	}
})

export type ReaderStore = ReturnType<typeof useReaderStore>

if (import.meta.hot) {
	import.meta.hot.accept(acceptHMRUpdate(useReaderStore, import.meta.hot))
}
