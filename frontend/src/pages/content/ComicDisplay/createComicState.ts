import type { Content, ReadingStatus, UserToContent } from '@/utils/api/types'
import { reactive, readonly, toRefs } from 'vue'
import { createPageLoader, getPagesInPreloadOrder, type PageLoaderState } from './usePageLoader'
import { contentApi } from '@/utils/api/content'
import type { PageDimensions } from './types'
import { API_URL } from '@/utils/fetch'
import { useDebounceFn } from '@vueuse/core'

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

	const updateProgress = useDebounceFn(() => {
		if (!state.content) return

		updateProgressPromise = updateProgressPromise
			.then(async () => {
				// Only update status if it's not set or is 'reading', so we don't
				// switch it back from 'completed' or other statuses
				let status: ReadingStatus | undefined = undefined
				if (!userData?.status || userData.status === 'reading') {
					if (state.page === state.pageDimensions.length - 1) {
						status = 'completed'
					} else {
						status = 'reading'
					}
				}

				userData = await contentApi.updateUserData(state.content!.id, {
					status,
					progress: {
						...userData?.progress,
						current_page: state.page,
					},
				})
			})
			.catch(err => {
				console.error('Failed to update reading progress', err)
			})
	}, 1000)

	contentApi
		.get(contentId)
		.then(content => {
			if (!state.handlers) {
				throw new Error('Comic handlers not set')
			}

			state.content = content
			userData = content.user_data ?? null
			state.pageDimensions = (content.meta.pages ?? []).map(p => ({
				width: p[1],
				height: p[2],
			}))
			// We just use `reactive` to turn it into UnwrapNestedRefs<_>
			state.loaders = reactive(
				state.pageDimensions.map((_, index) =>
					createPageLoader(index, getPageUrl(index), new AbortController().signal)
				)
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
			setPage(initialPage)
			state.handlers.onReady()
		})
		.catch(e => {
			console.error(e)
			state.error = e instanceof Error ? e.message : String(e)
			state.loading = false
		})

	function setPage(page: number) {
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
		dispose() {
			for (const loader of state.loaders) {
				loader.dispose()
			}
		},
	})
}

export type ComicState = ReturnType<typeof createComicState>
