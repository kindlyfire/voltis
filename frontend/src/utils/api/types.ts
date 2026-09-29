import type { DisplayMetadata } from './metadata'

export interface LibraryPreference {
    visibility?: 'show' | 'hide' | 'overflow'
}

export interface UserPreferences {
    libraries?: Record<string, LibraryPreference>
    tutorials?: { comicReader?: boolean; bookReader?: boolean }
    home?: { ignoreSeriesStatus?: boolean }
    reading?: { wordsPerMinute?: number; secondsPerPage?: number }
}

/** RFC 7396 merge patch over `UserPreferences`: `null` deletes a member. */
export interface PreferencesPatch {
    libraries?: Record<string, { visibility?: LibraryPreference['visibility'] | null } | null>
    tutorials?: { comicReader?: boolean | null; bookReader?: boolean | null }
    home?: { ignoreSeriesStatus?: boolean | null } | null
    reading?: { wordsPerMinute?: number | null; secondsPerPage?: number | null } | null
}

export interface User {
    id: string
    created_at: string
    updated_at: string
    username: string
    email: string | null
    permissions: string[]
    preferences: UserPreferences
    has_password: boolean
}

export type SessionMethod = 'password' | 'oidc' | 'proxy'

export interface Me extends User {
    session_method: SessionMethod
    can_logout: boolean
}

export interface UserUpsert {
    id?: string
    username: string
    email?: string | null
    password?: string
    permissions: string[]
}

export interface UpdateMe {
    username: string
    email?: string | null
    password?: string
}

export type IdentityProvider = 'oidc' | 'proxy'

export interface Identity {
    id: string
    provider: IdentityProvider
    issuer: string
    subject: string
    email: string | null
    created_at: string
}

export interface AppKey {
    id: string
    name: string
    key: string
    created_at: string
    last_used_at: string | null
    feeds: { v1: string; v2: string }
}

export interface IdentityLink {
    provider: IdentityProvider
    issuer: string
    subject: string
}

export type SettingType = 'bool' | 'int' | 'string' | 'string_list' | 'secret'

export interface Setting {
    key: string
    type: SettingType
    value: boolean | number | string | string[] | null
    secret: boolean
    set?: boolean
    help: string
}

export interface ProxyAuthStatus {
    enabled: boolean
    trusted_cidrs: string[]
    user_header: string
    email_header: string
    groups_header: string
}

export interface OidcPending {
    needs: 'confirm' | 'pick_username'
    username: string
    email: string
    match_username: string
    redirect?: string
}

export interface LogoutResponse {
    ok: boolean
    redirect_url: string
}

export type ScannerType = 'comics' | 'books'

/** Overlays the library's settings for the files under a source: a missing key inherits. */
export interface SourceSettings {
    auto_match?: Record<string, boolean>
}

export interface LibrarySource {
    path_uri: string
    settings: SourceSettings
}

export type BookSeriesInference = 'off' | 'conservative'

export interface LibrarySettings {
    book_series_inference: BookSeriesInference
    /** Match series with each metadata provider in the background; a missing provider is off. */
    auto_match: Record<string, boolean>
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
    settings: LibrarySettings
}

export interface FsMount {
    path: string
    fstype?: string
    /** The mount point didn't answer in time, which is typical of a dead network mount. */
    timed_out?: boolean
}

export interface FsRoots {
    mounts: FsMount[]
    mounts_error?: string
}

export interface FolderEntry {
    name: string
    /** Not resolved: a symlinked folder keeps the link's path. */
    path: string
    symlink: boolean
}

export interface FolderListing {
    /** Absolute and free of symlinks. */
    path: string
    parent: string | null
    entries: FolderEntry[]
    truncated: boolean
}

export interface ResolvedPath {
    input: string
    path: string | null
    /** Set when the input no longer exists and `path` is its nearest existing ancestor. */
    fallback_from?: string
    error?: string
}

export interface LibraryUpsert {
    id?: string
    name: string
    type: ScannerType
    sources: { path_uri: string; settings?: SourceSettings }[]
    settings?: LibrarySettings
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

/** Content an orphan can move to. */
export interface OrphanTarget {
    uri: string
    title: string | null
}

/** Metadata and provider links kept at a URI no content holds. */
export interface OrphanedMetadata {
    uri: string
    title: string | null
    /** Overridden field keys. */
    overrides: string[]
    links: {
        provider: string
        state: 'review' | 'unmatched' | 'linked' | 'ignored'
        external_id: string | null
        rejected: string[]
    }[]
}

export interface OrphansSummaryItem {
    library_id: string
    count: number
}

export interface OrphansFixRequest {
    delete?: string[]
    /** Orphaned URI -> content URI. */
    move?: Record<string, string>
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

/** A content's cover as the API versions it; see `coverUrl`. */
export interface Cover {
    id: string
    cover_version: string | null
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
    cover_version: string | null
    type: ContentType
    order: number | null
    order_parts: number[]
    meta: DisplayMetadata
    file_data: ContentFileData
    parent_id: string | null
    library_id: string
    children_count: number | null
    unread_children_count: number | null
    user_data: UserToContent | null
    /** Only on a single-item fetch. */
    length?: ContentLength
}

/** Words for books, pages for comics; `remaining` is what the user has left. */
export interface ContentLength {
    unit: 'words' | 'pages'
    total: number
    remaining: number
}

/** A "Recently Read" card: the item to read next, and its series unless standalone. */
export interface RecentlyReadEntry {
    item: Content
    series: Content | null
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
    covers: Cover[]
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

export type CustomList = Omit<CustomListPartial, 'covers'> & {
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
    /** Newest first. */
    recent?: ScanRecent[]
}

/** A series, or a standalone item, changed by the scan. */
export interface ScanRecent {
    id: string
    title: string
    cover_version: string | null
    added: number
    updated: number
    removed: number
    deleted: boolean
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
