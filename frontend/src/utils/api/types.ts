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
	type: ScannerType
	scanned_at: string | null
	sources: LibrarySource[]
}

export interface LibraryUpsert {
	id?: string
	type: ScannerType
	sources: LibrarySource[]
}
