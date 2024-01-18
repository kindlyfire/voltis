import type { InjectionKey } from 'vue'
import { getPageLoaders, type PageLoader } from './page-loader'
import type {
	ChapterData,
	ChapterListItem,
	Reader,
	ReaderHooks,
	ReaderProvider
} from './types'
import { createHooks } from 'hookable'

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

export function useReader(_provider: ReaderProvider): Reader {
	const state = createReaderState(_provider)
	const hooks = createHooks<ReaderHooks>()
	const setGlobalError = (err: any) => (state.globalError = '' + err)

	async function loadChapter(id: string) {
		const existing = state.chaptersData.find(c => c.id === id)
		if (existing) {
			state.provider.onChapterLoaded(existing)
			return
		}
		const chapterData = await state.provider.getChapterData(id)
		state.chaptersData.push(chapterData)
		state.chaptersPages.set(id, getPageLoaders(chapterData))
		state.page = chapterData.startPage
		if (!state.mode) state.mode = chapterData.suggestedMode
		state.provider.onChapterLoaded(chapterData)
	}

	async function load() {
		state.chapterId = state.provider.getChapterId()
		state.provider
			.getChapterList()
			.then(chapters => (state.chapters = chapters))
			.catch(setGlobalError)
		await loadChapter(state.chapterId)
	}
	Promise.resolve().then(load).catch(setGlobalError)

	function goToPage(page: number) {
		state.page = page
		state.provider.onPageChange(page)
		hooks.callHook('goToPage', page)
	}

	function switchChapter(
		dir: SwitchChapterDirection,
		pos = SwitchChapterPagePosition.Start
	) {
		const index = state.chapters.findIndex(c => c.id === state.chapterId)
		if (index === -1) {
			setGlobalError('Could not switch chapter: current chapter not found')
			return
		}
		const newIndex = index + (dir === SwitchChapterDirection.Forward ? -1 : 1)
		if (newIndex === -1 || newIndex === state.chapters.length) {
			return state.provider.onChapterChangeBeyondAvailable(dir)
		}
		const newChapter = state.chapters[newIndex]
		switchChapterById(newChapter.id, pos)
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
		state.provider.onChapterChange(chapter)
		loadChapter(chapter.id)
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

	return {
		state,
		activeChapter: computed(() => {
			return state.chaptersData.find(c => c.id === state.chapterId) ?? null
		}),
		hooks,
		reset() {},
		switchChapter,
		switchChapterById,
		goToPage
	}
}
