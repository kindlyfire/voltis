export interface LibraryPreference {
    visibility?: 'show' | 'hide' | 'overflow'
}

export interface UserPreferences {
    libraries?: Record<string, LibraryPreference>
    tutorials?: { comicReader?: boolean; bookReader?: boolean }
}

/** RFC 7396 merge patch over `UserPreferences`: `null` deletes a member. */
export interface PreferencesPatch {
    libraries?: Record<string, { visibility?: LibraryPreference['visibility'] | null } | null>
    tutorials?: { comicReader?: boolean | null; bookReader?: boolean | null }
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

export type ContentType = 'comic' | 'comic_series' | 'book' | 'book_series'

export type ReadingStatus = 'reading' | 'completed' | 'on_hold' | 'dropped' | 'plan_to_read'

export const READING_STATUS_LABELS: Record<ReadingStatus, string> = {
    reading: 'Reading',
    completed: 'Completed',
    on_hold: 'On Hold',
    dropped: 'Dropped',
    plan_to_read: 'Plan to Read',
}

/** Absolute normalized-text offset inside one EPUB document. Written and read
 * only by the client, so it never has to agree with the backend's counting. */
export interface BookLocator {
    version: 1
    href: string
    textOffset: number
    anchorId?: string
}

export interface ReadingProgress {
    current_page?: number
    progress_percent?: number
    book?: BookLocator
}

export interface UserToContent {
    starred: boolean
    status: ReadingStatus | null
    notes: string | null
    rating: number | null
    progress: ReadingProgress
}

export interface BrokenUserToContent extends UserToContent {
    id: string
    uri: string
    library_id: string | null
}

export interface BrokenRefsSummaryItem {
    library_id: string | null
    count: number
}

export interface LibraryUrisResponse {
    content_uris: string[]
    user_uris: string[]
}

export interface BrokenRefsFixRequest {
    delete?: string[]
    update?: Record<string, string>
}

export interface UserToContentUpdate {
    starred?: boolean
    status?: ReadingStatus | null
    notes?: string | null
    rating?: number | null
    progress?: ReadingProgress
}

export interface ContentFileData {
    pages?: Array<[filename: string, width: number, height: number]>
}

export interface StaffEntry {
    name: string
    role: string
}

export interface ContentMetadata {
    title?: string
    description?: string
    staff?: StaffEntry[]
    publisher?: string
    language?: string
    publication_date?: string
    series?: string
    number?: string
    volume?: number
    count?: number
    genre?: string
    age_rating?: string
    manga?: string
    format?: string
    imprint?: string
    web?: string
    notes?: string
    scan_information?: string
    black_and_white?: string
    series_group?: string
    alternate_series?: string
    alternate_number?: string
    alternate_count?: number
    series_index?: number
    mangabaka_id?: number
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
    unread_children_count: number | null
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
    has_status?: boolean
    has_rating?: boolean
    search?: string
    limit?: number
    offset?: number
    sort?:
        | 'order'
        | 'created_at'
        | 'progress_updated_at'
        | 'rating'
        | 'user_rating'
        | 'unread_children_count'
        | 'release_date'
        | 'title'
    sort_order?: 'asc' | 'desc'
}

export interface SpineItem {
    href: string
    title: string
    linear: boolean
    words: number
}

export interface TocEntry {
    id: string
    title: string
    depth: number
    href: string | null
    fragment: string
}

export interface BookStructure {
    spine: SpineItem[]
    /** Flat preorder; `depth` carries the nesting. */
    toc: TocEntry[]
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
    cover_content_ids: string[]
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

export type CustomList = Omit<CustomListPartial, 'cover_content_ids'> & {
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

export interface CustomListBulkCreateEntry {
    list_id: string
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
    ok: boolean
}

export interface ErrorResponse {
    error: string
}

export const TaskStatus = {
    PENDING: 0,
    IN_PROGRESS: 1,
    COMPLETED: 2,
    FAILED: 3,
    CANCELLED: 4,
} as const

export type TaskStatusValue = (typeof TaskStatus)[keyof typeof TaskStatus]

export interface ScanProgress {
    phase: 'walking' | 'parsing' | 'saving' | 'done'
    found: number
    total: number
    processed: number
    unchanged: number
    failed: number
    saved: { added: number; updated: number; removed: number }
    commit_seq: number
}

export interface ScanResult {
    added: number
    updated: number
    removed: number
    failed: number
    unchanged: number
    duration: number
}

export interface TaskSnapshot {
    id: string
    name: 'scan_library'
    status: TaskStatusValue
    input: {
        library_id: string
        library_type: string
        sources: string[]
        force: boolean
        filter_paths?: string[]
    }
    output: ScanResult | Record<string, never>
    progress: ScanProgress | null
    log_len: number
    created_at: string
    updated_at: string
}

export interface TaskLogs {
    offset: number
    text: string
    len: number
}

export interface ScanTaskIds {
    task_ids: string[]
}

export interface Task {
    id: string
    created_at: string
    updated_at: string
    name: string
    status: number
    input: Record<string, unknown>
    output: Record<string, unknown>
    logs: string | null
    user_id: string | null
    library_id: string | null
}

export interface TaskListParams {
    limit?: number
    offset?: number
    sort?: 'created_at' | 'updated_at'
    sort_order?: 'asc' | 'desc'
}

export interface MangaBakaSearchResult {
    id: number
    title: string
    type: string
    status: string
    year: number | null
    cover_url: string | null
    authors: string[]
    genres: string[]
}

export interface MangaBakaSearchResponse {
    data: MangaBakaSearchResult[]
}
