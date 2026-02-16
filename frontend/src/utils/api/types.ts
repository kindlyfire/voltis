export interface LibraryPreference {
    visibility?: 'show' | 'hide' | 'overflow'
}

export interface UserPreferences {
    libraries?: Record<string, LibraryPreference>
}

export interface User {
    id: string
    created_at: string
    updated_at: string
    username: string
    permissions: string[]
    preferences: UserPreferences
}

export interface UserUpsert {
    id?: string
    username: string
    password?: string
    permissions: string[]
}

export interface UpdateMe {
    username: string
    password?: string
    preferences?: UserPreferences
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
    starred: boolean
    status: ReadingStatus | null
    notes: string | null
    rating: number | null
    progress: ReadingProgress
}

export interface UserToContentUpdate {
    starred?: boolean
    status?: ReadingStatus | null
    notes?: string | null
    rating?: number | null
    progress?: ReadingProgress
}

export interface ContentFileData {
    /** [filename, width, height] */
    pages?: Array<[string, number, number]>
}

export interface ContentMetadata {
    // Shared fields
    authors?: string[]
    description?: string
    publisher?: string
    language?: string
    publication_date?: string
    // Comic-specific fields
    title?: string
    series?: string
    writer?: string
    penciller?: string
    inker?: string
    colorist?: string
    letterer?: string
    cover_artist?: string
    editor?: string
    genre?: string
    age_rating?: string
    manga?: string
    characters?: string
    teams?: string
    locations?: string
    story_arc?: string
    series_group?: string
    format?: string
    imprint?: string
    web?: string
    notes?: string
    scan_information?: string
    black_and_white?: string
    community_rating?: number
    review?: string
    main_character_or_team?: string
    alternate_series?: string
    alternate_number?: string
    alternate_count?: number
    count?: number
    number?: string
    volume?: number
}

export interface MetadataLayer {
    source: string
    data: ContentMetadata
    raw: Record<string, unknown>
}

export interface MetadataLayersResponse {
    merged: ContentMetadata
    layers: MetadataLayer[]
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
    file_data: ContentFileData
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
    starred?: boolean
    search?: string
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

export interface DownloadInfo {
    file_count: number
    total_size: number | null
}

export type CustomListVisibility = 'public' | 'private' | 'unlisted'

export interface CustomListPartial {
    id: string
    created_at: string
    updated_at: string
    name: string
    description: string | null
    visibility: CustomListVisibility
    user_id: string
    entry_count: number | null
    cover_uris: string[]
}

export interface CustomListEntry {
    id: string
    created_at: string
    updated_at: string
    library_id: string
    uri: string
    content: Content | null
    notes: string | null
    order: number | null
}

export type CustomList = Omit<CustomListPartial, 'cover_uris'> & {
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
