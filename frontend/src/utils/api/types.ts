export interface User {
	id: string
	created_at: string
	updated_at: string
	username: string
	permissions: string[]
}

export interface UserUpsert {
	id?: string
	username: string
	password?: string
	permissions: string[]
}

export type ScannerType = 'comics' | 'books'

export interface LibrarySource {
	path_uri: string
}

export interface Library {
	id: string
	created_at: string
	updated_at: string
	name: string
	type: ScannerType
	scanned_at: string | null
	sources: LibrarySource[]
}

export interface LibraryUpsert {
	id?: string
	name: string
	type: ScannerType
	sources: LibrarySource[]
}

export interface ScanResult {
	library_id: string
	added: number
	updated: number
	removed: number
	unchanged: number
}

export type ContentType = 'comic' | 'comic_series' | 'book' | 'book_series'

export interface ContentMetadata {
	/** [filename, width, height] */
	pages?: Array<[string, number, number]>
	authors?: string[]
	description?: string
	publisher?: string
	language?: string
	publication_date?: string
}

export interface Content {
	id: string
	created_at: string
	updated_at: string
	uri_part: string
	title: string
	valid: boolean
	file_uri: string
	file_mtime: string | null
	file_size: number | null
	cover_uri: string | null
	type: ContentType
	order: number | null
	order_parts: number[]
	meta: ContentMetadata
	parent_id: string | null
	library_id: string
}

export interface ContentListParams {
	parent_id?: string
	library_id?: string
	type?: ContentType[]
	valid?: boolean
}

export interface BookChapter {
	id: string
	href: string
	title: string | null
	linear: boolean
}
