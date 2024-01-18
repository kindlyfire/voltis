import type { SwitchChapterDirection } from './use-reader'

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
	collectionLink: string
}

export interface ChapterListItem {
	id: string
	title: string
}

export interface ReaderProvider {
	getChapterId(): string
	fetchChapterData(id: string): Promise<ChapterData>
	/**
	 * Chapters are expected to be returned in REVERSE order of reading. The
	 * first chapter to read is the last one in the array.
	 */
	getChapterList(): Promise<ChapterListItem[]>
	onPageChange(page: number): void
	beforeChapterChange(chapter: ChapterListItem): void
	afterChapterChange(chapter: ChapterData): void
	onChapterChangeBeyondAvailable(direction: SwitchChapterDirection): void
}
