import {
	ref,
	shallowRef,
	computed,
	watch,
	onUnmounted,
	toValue,
	type InjectionKey,
	type MaybeRefOrGetter,
	type Ref,
	type ShallowRef,
} from 'vue'
import type { PageInfo, ReaderMode, SiblingsInfo, SiblingContent } from './types'
import { createPageLoader, getPagesInPreloadOrder, type PageLoaderState } from './use-page-loader'
import { useRouter } from 'vue-router'

const PAGE_CACHE_WINDOW = 5
const PRELOAD_COUNT = 8
const PRELOAD_CONCURRENCY = 3

export interface ReaderState {
	contentId: string
	pages: PageInfo[]
	currentPage: Ref<number>
	mode: Ref<ReaderMode>
	loaders: ShallowRef<PageLoaderState[]>
	scrollRef: Ref<HTMLElement | null>
	progress: Ref<number>
	sidebarOpen: Ref<boolean>
	siblings: Ref<SiblingsInfo | null>
	prevSibling: Ref<SiblingContent | null>
	nextSibling: Ref<SiblingContent | null>
	handleNext: () => void
	handlePrev: () => void
	goToPage: (page: number) => void
	getLoader: (index: number) => PageLoaderState | undefined
	goToSibling: (id: string, fromEnd?: boolean) => void
}

export const readerKey = Symbol('reader') as InjectionKey<ReaderState>

export interface UseReaderOptions {
	contentId: string
	pages: PageInfo[]
	siblings?: MaybeRefOrGetter<SiblingsInfo | null>
	initialMode?: ReaderMode
	getPageUrl: (index: number) => string
	onPageChange?: (page: number) => void
	onReachStart?: () => void
	onReachEnd?: () => void
	onGoToSibling?: (id: string, fromEnd?: boolean) => void
}

export function useReader(options: UseReaderOptions): ReaderState {
	console.log('useReader initialized with options:', options)

	const {
		contentId,
		pages,
		siblings: siblingsOption,
		initialMode = 'paged',
		getPageUrl,
		onPageChange,
		onReachStart,
		onReachEnd,
		onGoToSibling,
	} = options

	const router = useRouter()

	const setPageParam = (page: number) => {
		router.replace({ query: page === 0 ? {} : { page } })
	}

	const currentPage = computed({
		get() {
			const page = router.currentRoute.value.query.page
			if (page === 'last') {
				setPageParam(pages.length - 1)
				return pages.length - 1
			} else if (page === '') {
				return 0
			}

			const parsed = typeof page === 'string' ? parseInt(page, 10) : NaN
			if (isNaN(parsed) || parsed < 0 || parsed >= pages.length) {
				setPageParam(0)
				return 0
			}

			return parsed
		},
		set(v: number) {
			setPageParam(v)
		},
	})

	const mode = ref<ReaderMode>(initialMode)
	const scrollRef = ref<HTMLElement | null>(null)
	const loaders = shallowRef<PageLoaderState[]>([])
	const abortController = shallowRef(new AbortController())

	const progress = computed(() => {
		if (pages.length === 0) return 0
		return ((currentPage.value + 1) / pages.length) * 100
	})

	const siblings = computed(() => toValue(siblingsOption) ?? null)

	const prevSibling = computed(() => {
		const s = siblings.value
		if (!s || s.currentIndex <= 0) return null
		return s.items[s.currentIndex - 1] ?? null
	})

	const nextSibling = computed(() => {
		const s = siblings.value
		if (!s || s.currentIndex >= s.items.length - 1) return null
		return s.items[s.currentIndex + 1] ?? null
	})

	// Initialize loaders for all pages
	function initializeLoaders() {
		loaders.value = pages.map((_, index) =>
			createPageLoader(index, getPageUrl(index), abortController.value.signal)
		)
	}

	// Cleanup loaders outside the cache window
	function cleanupDistantLoaders() {
		for (const loader of loaders.value) {
			if (Math.abs(loader.index - currentPage.value) > PAGE_CACHE_WINDOW) {
				loader.dispose()
			}
		}
	}

	// Preload pages around the current page
	function preloadPages() {
		const order = getPagesInPreloadOrder(pages.length, currentPage.value)
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

	// Watch page changes
	watch(currentPage, (newPage, oldPage) => {
		if (newPage !== oldPage) {
			onPageChange?.(newPage)
			cleanupDistantLoaders()
			preloadPages()
		}
	})

	// Initialize on mount
	initializeLoaders()
	preloadPages()

	// Cleanup on unmount
	onUnmounted(() => {
		abortController.value.abort()
		for (const loader of loaders.value) {
			loader.dispose()
		}
	})

	function goToPage(page: number) {
		const clamped = Math.min(Math.max(0, page), pages.length - 1)
		currentPage.value = clamped
	}

	function handleNext() {
		if (mode.value === 'paged') {
			if (currentPage.value >= pages.length - 1) {
				onReachEnd?.()
			} else {
				currentPage.value++
			}
		} else {
			// Longstrip: handled by ReaderModeLongstrip
			if (isAtBottom()) {
				onReachEnd?.()
			} else {
				scrollByViewport(0.9)
			}
		}
	}

	function handlePrev() {
		if (mode.value === 'paged') {
			if (currentPage.value <= 0) {
				onReachStart?.()
			} else {
				currentPage.value--
			}
		} else {
			// Longstrip: handled by ReaderModeLongstrip
			if (isAtTop()) {
				onReachStart?.()
			} else {
				scrollByViewport(-0.9)
			}
		}
	}

	function isAtBottom(): boolean {
		const el = scrollRef.value
		if (!el) return true
		return el.scrollTop + el.clientHeight >= el.scrollHeight - 10
	}

	function isAtTop(): boolean {
		const el = scrollRef.value
		if (!el) return true
		return el.scrollTop <= 10
	}

	function scrollByViewport(factor: number) {
		const el = scrollRef.value
		if (!el) return
		el.scrollBy({ top: el.clientHeight * factor, behavior: 'smooth' })
	}

	function getLoader(index: number): PageLoaderState | undefined {
		return loaders.value[index]
	}

	function goToSibling(id: string, fromEnd = false) {
		onGoToSibling?.(id, fromEnd)
	}

	return {
		contentId,
		pages,
		currentPage,
		mode,
		loaders,
		scrollRef,
		progress,
		sidebarOpen: ref(false),
		siblings,
		prevSibling,
		nextSibling,
		handleNext,
		handlePrev,
		goToPage,
		getLoader,
		goToSibling,
	}
}
