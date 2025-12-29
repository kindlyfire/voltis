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
    content_count: number | null
    root_content_count: number | null
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

export type ReadingStatus = 'reading' | 'completed' | 'on_hold' | 'dropped' | 'plan_to_read'

export interface ReadingProgress {
    current_page?: number
    progress_percent?: number
}

export interface UserToContent {
    status: ReadingStatus | null
    notes: string | null
    rating: number | null
    progress: ReadingProgress
}

export interface UserToContentUpdate {
    status?: ReadingStatus | null
    notes?: string | null
    rating?: number | null
    progress?: ReadingProgress
}

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
    file_uri: string | null
    file_mtime: string | null
    file_size: number | null
    cover_uri: string | null
    type: ContentType
    order: number | null
    order_parts: number[]
    meta: ContentMetadata
    parent_id: string | null
    library_id: string
    children_count: number | null
    user_data: UserToContent | null
}

export interface Paginated<T> {
    data: T[]
    total: number
}

export interface ContentListParams {
    parent_id?: string
    library_id?: string
    type?: ContentType[]
    valid?: boolean
    reading_status?: ReadingStatus
    limit?: number
    offset?: number
    sort?: 'order' | 'created_at' | 'progress_updated_at'
    sort_order?: 'asc' | 'desc'
}

export interface BookChapter {
    id: string
    href: string
    title: string | null
    linear: boolean
}

export type CustomListVisibility = 'public' | 'private' | 'unlisted'

export interface CustomList {
    id: string
    created_at: string
    updated_at: string
    name: string
    description: string | null
    visibility: CustomListVisibility
    user_id: string
    entry_count: number | null
}

export interface CustomListEntry {
    id: string
    created_at: string
    updated_at: string
    library_id: string
    uri: string
    content_id: string | null
    notes: string | null
    order: number | null
}

export interface CustomListDetail extends CustomList {
    entries: CustomListEntry[]
}

export interface CustomListUpsert {
    name: string
    description?: string | null
    visibility: CustomListVisibility
}

export interface CustomListEntryCreate {
    content_id: string
    notes?: string | null
}

export interface CustomListEntryUpdate {
    notes?: string | null
    order?: number | null
}

export interface CustomListReorderRequest {
    ctc_ids: string[]
}

export interface OkResponse {
    success: boolean
}
