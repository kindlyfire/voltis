import { ref, computed, type Ref } from 'vue'
import { acceptHMRUpdate, defineStore } from 'pinia'
import { useRouter } from 'vue-router'
import type { ReaderMode, SiblingsInfo } from './types'
import { contentApi } from '@/utils/api/content'
import { keepPreviousData } from '@tanstack/vue-query'
import { useLocalStorage } from '@/utils/localStorage'
import { arrayAtNowrap, getLayoutTop } from '@/utils/misc'
import { createComicState, type ComicState } from './createComicState'
import z from 'zod'

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

		// We detect longstrips by taking the average aspect ratio of pages
		const pages = state.value?.pageDimensions || []
		if (pages.length === 0) return 'paged'
		const totalAspectRatio = pages.reduce((sum, p) => sum + p.height / p.width, 0)
		const avgAspectRatio = totalAspectRatio / pages.length
		console.log('Avg aspect ratio:', avgAspectRatio)
		return avgAspectRatio > 1.6 ? 'longstrip' : 'paged'
	})

	const state: Ref<ComicState | null> = ref(null)
	const content = computed(() => state.value?.content || null)

	const qSiblings = contentApi.useList(
		() => {
			if (content.value?.parent_id)
				return { parent_id: content.value.parent_id, sort: 'order', sort_order: 'asc' }
		},
		{
			placeholderData: keepPreviousData,
		}
	)
	const siblings = computed<SiblingsInfo>(() => {
		const siblings = qSiblings.data.value?.data
		if ((content.value && !content.value?.parent_id) || !siblings)
			return {
				items: [],
				currentIndex: 0,
			}
		const items = siblings.map(c => ({ id: c.id, title: c.title, order: c.order }))
		const currentIndex = items.findIndex(item => item.id === state.value?.contentId)
		return {
			items,
			currentIndex: currentIndex >= 0 ? currentIndex : 0,
		}
	})

	const progress = computed(() => {
		const pagesVal = state.value?.pageDimensions
		if (!pagesVal?.length) return 0
		return (((state.value?.page ?? 0) + 1) / pagesVal.length) * 100
	})

	function dispose() {
		sidebarOpen.value = false
		const s = state.value
		if (s) {
			s.dispose()
			state.value = null
		}
	}

	function setMode(mode: ReaderMode | null) {
		const c = state.value?.content
		if (!c) return

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
		if (state.value) {
			state.value.dispose()
		}
		state.value = createComicState(options.contentId, options.initialPage)
		state.value.setHandlers({
			onReady: () => {
				if (mode.value === 'longstrip') {
					requestAnimationFrame(() => {
						if (state.value!.initialPage === 'last') {
							window.scrollTo({
								top: document.body.scrollHeight,
								behavior: 'instant',
							})
							return
						}
						goToPage(state.value!.page, 'instant')
					})
				}

				// This is to replace "last" or "resume" in the URL with the
				// actual page number
				const page = state.value!.page
				router.replace({ query: page === 0 ? {} : { page: page + 1 } })
			},
		})
	}

	function setPage(page: number) {
		if (page === state.value?.page) {
			return
		}
		router.replace({ query: page === 0 ? {} : { page: page + 1 } })
		state.value?.setPage(page)
	}

	function goToPage(page: number | null = null, behavior: ScrollBehavior = 'instant') {
		if (page === null) {
			page = state.value?.page ?? 0
		}
		setPage(page)
		if (mode.value === 'longstrip') {
			const pageEl = document.getElementById(`longstrip-page-${page}`)
			if (pageEl) {
				window.scrollTo({
					top: pageEl.offsetTop - getLayoutTop(),
					behavior,
				})
			}
		}
	}

	function goToSibling(id: (string & {}) | 'next' | 'prev', fromEnd = false) {
		if (id === 'next' || id === 'prev') {
			const sibling = arrayAtNowrap(
				siblings.value.items,
				siblings.value.currentIndex + (id === 'next' ? 1 : -1)
			)
			if (sibling) {
				id = sibling.id
			} else {
				return
			}
		}
		router.push({
			name: 'content',
			params: { id },
			query: fromEnd ? { page: 'last' } : {},
		})
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
		qSiblings,
		siblings,
		progress,

		// Actions
		setContent,
		goToPage,
		goToSibling,
		dispose,
		setPage,
	}
})

export type ReaderStore = ReturnType<typeof useReaderStore>

if (import.meta.hot) {
	import.meta.hot.accept(acceptHMRUpdate(useReaderStore, import.meta.hot))
}
