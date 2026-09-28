import type { Content } from '@/utils/api/types'

export interface ContentProgress {
    fraction: number
    label: string
}

function percentProgress(percent: number | undefined): ContentProgress | null {
    if (typeof percent !== 'number' || percent <= 0 || percent >= 100) return null
    return {
        fraction: percent / 100,
        label: `${Math.min(99, Math.max(1, Math.round(percent)))}%`,
    }
}

/** `null` hides the bar: untouched, completed, or not enough data. */
export function contentProgress(content: Content): ContentProgress | null {
    const status = content.user_data?.status
    if (status === 'completed' || status === 'dropped') return null

    if (content.type === 'comic') {
        const page = content.user_data?.progress?.current_page
        if (typeof page !== 'number' || page <= 0) return null
        const pages = content.file_data?.pages?.length ?? 0
        // List responses omit `file_data`; fall back to the percent the reader writes.
        if (pages <= 0) return percentProgress(content.user_data?.progress?.progress_percent)
        // Comic pages are 0-based, so the last page means completed.
        if (page >= pages - 1) return null
        return { fraction: (page + 1) / pages, label: `Page ${page + 1} / ${pages}` }
    }

    if (content.type === 'book') {
        return percentProgress(content.user_data?.progress?.progress_percent)
    }

    const total = content.children_count ?? 0
    // `dropped` counts as read and a partially-read child as unread, matching the
    // backend's unread count.
    const read = total - (content.unread_children_count ?? 0)
    if (total <= 0 || read <= 0 || read >= total) return null
    return { fraction: read / total, label: `${read} / ${total} chapters` }
}

/** The series' `read / total` children, with the unread count's semantics. */
export function seriesPosition(series: Content): { read: number; total: number } | null {
    const total = series.children_count ?? 0
    if (total <= 0) return null
    return { read: total - (series.unread_children_count ?? 0), total }
}
