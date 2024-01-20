export interface ReaderDataPage {
	name: string
	width: number
	height: number
	url: string
}

export interface ReaderDataPageLoaded extends ReaderDataPage {
	blobUrl: string
}

export enum SwitchChapterDirection {
	Forward,
	Backward
}
export enum SwitchChapterPagePosition {
	Start,
	End
}

export interface ReaderHooks {
	goToPage(page: number): void
	beforeChapterChange(chapter: ChapterListItem): void
}

export interface ChapterData<T = any> {
	pages: Array<ReaderDataPage>
	mode: 'pages' | 'longstrip'
	page: number
	id: string
	title: string
	collection: {
		id: string
		title: string
		link: string
	}
	custom: T
}

export interface ChapterListItem {
	id: string
	title: string
}

interface ReaderEvent<T> {
	custom: T
}
interface PageChangeEvent<T> extends ReaderEvent<T> {
	chapter: ChapterData<T>
	value: number
	oldValue: number | null
}
interface BeforeChapterChangeEvent<T> extends ReaderEvent<T> {
	chapter: ChapterListItem
}
interface AfterChapterChangeEvent<T> extends ReaderEvent<T> {
	chapter: ChapterData<T>
}
interface OnChapterChangeBeyondAvailableEvent<T> {
	custom: T | null
	chapter: ChapterData<T> | null
	direction: SwitchChapterDirection
}

export interface ReaderProvider<T = {}> {
	getChapterId(): string
	fetchChapterData(id: string): Promise<ChapterData<T>>
	/**
	 * Chapters are expected to be returned in REVERSE order of reading. The
	 * first chapter to read is the last one in the array.
	 */
	getChapterList(): Promise<ChapterListItem[]>
	onPageChange(ev: PageChangeEvent<T>): void
	beforeChapterChange(ev: BeforeChapterChangeEvent<T>): void
	afterChapterChange(ev: AfterChapterChangeEvent<T>): void
	onChapterChangeBeyondAvailable(
		ev: OnChapterChangeBeyondAvailableEvent<T>
	): void
}
