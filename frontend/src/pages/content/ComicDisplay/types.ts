export interface PageDimensions {
	width: number
	height: number
}

export type ReaderMode = 'paged' | 'longstrip'

export interface SiblingContent {
	id: string
	title: string
	order: number | null
}

export interface SiblingsInfo {
	items: SiblingContent[]
	currentIndex: number
}
