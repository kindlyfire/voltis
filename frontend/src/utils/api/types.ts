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

export type ContentType = 'comic' | 'comic_series' | 'book' | 'book_series'

export interface Content {
	id: string
	created_at: string
	updated_at: string
	uri_part: string
	title: string
	valid: boolean
	file_uri: string
	cover_uri: string | null
	type: ContentType
	order: number | null
	order_parts: number[]
	metadata_: Record<string, unknown> | null
	file_modified_at: string | null
	parent_id: string | null
	library_id: string
}

export interface ContentListParams {
	parent_id?: string
	library_id?: string
	type?: ContentType[]
	valid?: boolean
}
