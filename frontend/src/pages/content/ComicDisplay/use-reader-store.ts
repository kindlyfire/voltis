import { ref, shallowRef, computed, watch } from 'vue'
import { defineStore } from 'pinia'
import { useRouter } from 'vue-router'
import type { PageInfo, ReaderMode, SiblingsInfo, SiblingContent } from './types'
import { createPageLoader, getPagesInPreloadOrder, type PageLoaderState } from './use-page-loader'
import { contentApi } from '@/utils/api/content'

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
	const mode = ref<ReaderMode>('paged')
	const contentId = ref<string>('')
	const pages = shallowRef<PageInfo[]>([])
	const getPageUrlFn = shallowRef<(index: number) => string>(() => '')
	const onReachStartFn = shallowRef<(() => void) | undefined>()
	const onReachEndFn = shallowRef<(() => void) | undefined>()
	const onGoToSiblingFn = shallowRef<((id: string, fromEnd?: boolean) => void) | undefined>()

	const loaders = shallowRef<PageLoaderState[]>([])
	const abortController = shallowRef<AbortController | null>(null)
	const scrollRef = ref<HTMLElement | null>(null)

	const qContent = contentApi.useGet(contentId)
	const content = qContent.data
	const qSiblings = contentApi.useList(() => ({
		parent_id: content.value?.parent_id ?? undefined,
	}))
	const siblings = computed<SiblingsInfo | null>(() => {
		const siblings = qSiblings.data.value
		if (!content.value?.parent_id || !siblings) return null
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
				pages.value = newContent.meta.pages.map(p => ({
					width: p[1],
					height: p[2],
				}))
				_initializeLoaders()
				_preloadPages()
			}
		},
		{ immediate: true }
	)

	const currentPage = computed({
		get() {
			if (router.currentRoute.value.params.id !== contentId.value) {
				return 0
			}

			const pagesVal = pages.value
			const page = router.currentRoute.value.query.page
			if (page === 'last') {
				return _setPageParam(pagesVal.length - 1)
			} else if (page === '') {
				return 0
			}

			const parsed = typeof page === 'string' ? parseInt(page, 10) : NaN
			if (isNaN(parsed) || parsed < 0 || parsed >= pagesVal.length) {
				return _setPageParam(0)
			}

			return parsed
		},
		set(v: number) {
			_setPageParam(v)
		},
	})

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

		// Initialize new loaders
		abortController.value = new AbortController()
		// _initializeLoaders()
		// _preloadPages()
	}

	function disposeLoaders() {
		abortController.value?.abort()
		for (const loader of loaders.value) {
			loader.dispose()
		}
		loaders.value = []
	}

	function goToPage(page: number) {
		const pagesVal = pages.value
		const clamped = Math.min(Math.max(0, page), pagesVal.length - 1)
		currentPage.value = clamped
	}

	function handleNext() {
		const pagesVal = pages.value
		if (mode.value === 'paged') {
			if (currentPage.value >= pagesVal.length - 1) {
				onReachEndFn.value?.()
			} else {
				currentPage.value++
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
		if (mode.value === 'paged') {
			if (currentPage.value <= 0) {
				onReachStartFn.value?.()
			} else {
				currentPage.value--
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

	// Watch page changes to cleanup distant loaders and preload
	watch(currentPage, (newPage, oldPage) => {
		if (newPage !== oldPage) {
			_cleanupDistantLoaders()
			_preloadPages()
		}
	})

	return {
		// Persistent state
		sidebarOpen,
		mode,

		// Content state (readonly)
		contentId: computed(() => contentId.value),
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
		disposeLoaders,
		goToPage,
		handleNext,
		handlePrev,
		getLoader,
		goToSibling,
	}
})

export type ReaderStore = ReturnType<typeof useReaderStore>
