import { ref, shallowRef, computed, type Ref } from 'vue'
import { acceptHMRUpdate, defineStore } from 'pinia'
import { useRouter } from 'vue-router'
import type { ReaderMode, SiblingsInfo } from './types'
import { contentApi } from '@/utils/api/content'
import { keepPreviousData } from '@tanstack/vue-query'
import { useLocalStorage } from '@/utils/localStorage'
import { arrayAtNowrap, getLayoutTop } from '@/utils/misc'
import { createComicState, type ComicState } from './createComicState'

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

export interface ReaderContentOptions {
	contentId: string
	initialPage: number | 'last' | 'resume'
}

export const useReaderStore = defineStore('reader', () => {
	const router = useRouter()

	const sidebarOpen = ref(false)
	const { value: settings } = useLocalStorage('reader:comics', parseComicSettings)

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

	function setContent(options: ReaderContentOptions) {
		if (options.contentId === state.value?.contentId) {
			return
		}
		dispose()
		state.value = createComicState(options.contentId, options.initialPage)
		state.value.setHandlers({
			onReady: () => {
				if (settings.value.mode === 'longstrip') {
					requestAnimationFrame(() => {
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
		if (settings.value.mode === 'longstrip') {
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
