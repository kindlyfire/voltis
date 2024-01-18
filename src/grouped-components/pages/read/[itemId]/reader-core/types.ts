import type {
	ReaderState,
	SwitchChapterDirection,
	SwitchChapterPagePosition
} from './use-reader'
import { Hookable } from 'hookable'

export interface ReaderDataPage {
	name: string
	width: number
	height: number
	url: string
}

export interface ReaderDataPageLoaded extends ReaderDataPage {
	blobUrl: string
}

export interface ReaderHooks {
	goToPage(page: number): void
}

export interface ChapterData {
	pages: Array<ReaderDataPage>
	suggestedMode: 'pages' | 'longstrip'
	startPage: number
	id: string
	title: string
	collectionId: string
	collectionTitle: string
}

export interface ChapterListItem {
	id: string
	title: string
}

export interface ReaderProvider {
	getChapterId(): string

	getChapterData(id: string): Promise<ChapterData>

	/**
	 * Chapters are expected to be returned in REVERSE order of reading. The
	 * first chapter to read is the last one in the array.
	 */
	getChapterList(): Promise<ChapterListItem[]>

	onPageChange(page: number): void

	onChapterChange(chapter: ChapterListItem): void

	onChapterLoaded(chapter: ChapterData): void

	onChapterChangeBeyondAvailable(direction: SwitchChapterDirection): void
}

export interface Reader {
	state: ReaderState
	activeChapter: ComputedRef<ChapterData | null>
	hooks: Hookable<ReaderHooks>

	reset(): void

	switchChapter(
		dir: SwitchChapterDirection,
		pos?: SwitchChapterPagePosition
	): void

	switchChapterById(id: string, pos?: SwitchChapterPagePosition): void

	goToPage(page: number): void
}
