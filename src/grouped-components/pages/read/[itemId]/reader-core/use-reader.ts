import type { InjectionKey } from 'vue'
import { getPageLoaders, type PageLoader } from './page-loader'
import type {
	ChapterData,
	ChapterListItem,
	ReaderHooks,
	ReaderProvider
} from './types'
import { createHooks } from 'hookable'
import pMemoize from 'p-memoize'

export const readerKey = Symbol() as InjectionKey<Reader>

function createReaderState(provider: ReaderProvider) {
	return reactive({
		provider,
		globalError: null as string | null,
		chapterId: '',
		chaptersData: [] as ChapterData[],
		chaptersPages: new Map<string, PageLoader[]>(),
		chapters: [] as ChapterListItem[],
		mode: null as 'pages' | 'longstrip' | null,
		page: 0,

		menuOpen: false,
		mainRef: null as HTMLElement | null,
		scrollRef: null as HTMLElement | null
	})
}
export type ReaderState = ReturnType<typeof createReaderState>

export enum SwitchChapterDirection {
	Forward,
	Backward
}
export enum SwitchChapterPagePosition {
	Start,
	End
}

export type Reader = ReturnType<typeof useReader>
export function useReader(_provider: ReaderProvider) {
	const state = createReaderState(_provider)
	const hooks = createHooks<ReaderHooks>()
	const setGlobalError = (err: any) => (state.globalError = '' + err)

	const activeChapter = computed(() => {
		return state.chaptersData.find(c => c.id === state.chapterId) ?? null
	})

	async function __loadChapterData(id: string) {
		const existing = state.chaptersData.find(c => c.id === id)
		if (existing) {
			return existing
		}
		const chapterData = await state.provider.fetchChapterData(id)
		state.chaptersData.push(chapterData)
		state.chaptersPages.set(id, getPageLoaders(chapterData))
		return chapterData
	}
	const _loadChapterData = pMemoize(__loadChapterData, {
		cache: false // cache pending only
	})

	async function _loadChapter(id: string) {
		const chapterData = await _loadChapterData(id)
		state.page = chapterData.startPage
		if (!state.mode) state.mode = chapterData.suggestedMode
		state.provider.afterChapterChange(chapterData)
	}

	let preloadChapterPromise: Promise<any> | null = null
	function _preloadChapter() {
		if (preloadChapterPromise) return preloadChapterPromise
		const chapter = _getChapterInDirection(SwitchChapterDirection.Forward)
		if (!chapter) return
		preloadChapterPromise = _loadChapterData(chapter!.id).finally(
			() => (preloadChapterPromise = null)
		)
	}

	async function _initialize() {
		state.chapterId = state.provider.getChapterId()
		state.provider
			.getChapterList()
			.then(chapters => (state.chapters = chapters))
			.catch(setGlobalError)
		await _loadChapter(state.chapterId)
	}
	Promise.resolve().then(_initialize).catch(setGlobalError)

	function goToPage(page: number) {
		setPageTo(page)
		if (state.page !== page) return
		hooks.callHook('goToPage', page)
	}

	function setPageTo(page: number) {
		if (page < 0 || page > (activeChapter.value?.pages.length ?? 0)) return
		state.page = page
		state.provider.onPageChange(page)

		const pagesLeft = activeChapter.value!.pages.length - page
		if (pagesLeft < 10) _preloadChapter()
	}

	function _getChapterInDirection(dir: SwitchChapterDirection) {
		const index = state.chapters.findIndex(c => c.id === state.chapterId)
		if (index === -1) {
			setGlobalError('Could not switch chapter: current chapter not found')
			return
		}
		const newIndex = index + (dir === SwitchChapterDirection.Forward ? -1 : 1)
		if (newIndex === -1 || newIndex === state.chapters.length) {
			return null
		}
		return state.chapters[newIndex]
	}

	function switchChapter(
		dir: SwitchChapterDirection,
		pos = SwitchChapterPagePosition.Start
	) {
		const chapter = _getChapterInDirection(dir)
		if (!chapter) {
			return state.provider.onChapterChangeBeyondAvailable(dir)
		}
		switchChapterById(chapter.id, pos)
	}

	function switchChapterById(
		id: string,
		pos = SwitchChapterPagePosition.Start
	) {
		const chapter = state.chapters.find(c => c.id === id)
		if (!chapter) {
			setGlobalError('Could not switch chapter: chapter not found')
			return
		}
		state.chapterId = chapter.id
		state.provider.beforeChapterChange(chapter)
		state.page = 0
		_loadChapter(chapter.id)
			.then(() => {
				const pages = state.chaptersPages.get(chapter.id)!
				state.page = Math.max(
					0,
					pos === SwitchChapterPagePosition.Start ? 0 : pages.length - 1
				)
				goToPage(state.page)
			})
			.catch(setGlobalError)
	}

	function switchMode(mode?: 'pages' | 'longstrip') {
		mode = mode ?? (state.mode === 'pages' ? 'longstrip' : 'pages')
		state.mode = mode
	}

	return {
		state,
		activeChapter,
		hooks,
		reset() {},
		switchChapter,
		switchChapterById,
		switchMode,
		goToPage,
		setPageTo
	}
}
