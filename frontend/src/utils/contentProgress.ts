import type { Content, ContentType } from '@/utils/api/types'

export interface ContentProgress {
    fraction: number
    label: string
}

function percentProgress(percent: number | undefined): ContentProgress | null {
    if (typeof percent !== 'number') return null
    const clamped = Math.min(100, Math.max(0, percent))
    // Only the ends round to 0% and 100%.
    const shown =
        clamped > 0 && clamped < 100 ? Math.min(99, Math.max(1, Math.round(clamped))) : clamped
    return { fraction: clamped / 100, label: `${shown}%` }
}

/** `null` hides the bar: no saved position, completed or dropped, or not enough data. A saved
 * start or end still shows. */
export function contentProgress(content: Content): ContentProgress | null {
    const status = content.user_data?.status
    if (status === 'completed' || status === 'dropped') return null

    if (content.type === 'comic') {
        const page = content.user_data?.progress?.current_page
        if (typeof page !== 'number') return null
        const pages = content.file_data?.pages?.length ?? 0
        // List responses omit `file_data`; fall back to the percent the reader writes.
        if (pages <= 0) return percentProgress(content.user_data?.progress?.progress_percent)
        // As the reader counts and clamps it: pages before this one are read, which agrees with
        // the time left.
        const shown = Math.min(Math.max(page, 0), pages - 1)
        return { fraction: shown / pages, label: `Page ${shown + 1} / ${pages}` }
    }

    if (content.type === 'book') {
        return percentProgress(content.user_data?.progress?.progress_percent)
    }

    const position = seriesPosition(content)
    if (!position || position.read <= 0 || position.read >= position.total) return null
    return { fraction: position.read / position.total, label: seriesReadLabel(content)! }
}

/** "volume", "chapters": what a series' children are called. */
export function childNoun(type: ContentType, n: number): string {
    const noun = type === 'book_series' ? 'volume' : 'chapter'
    return n === 1 ? noun : `${noun}s`
}

/** The series' valid children done with (completed or dropped), as the unread count leaves out. */
export function seriesPosition(series: Content): { read: number; total: number } | null {
    const total = series.children_count ?? 0
    if (total <= 0) return null
    return { read: total - (series.unread_children_count ?? 0), total }
}

/** "11/12 read · 1 dropped". */
export function seriesReadLabel(
    series: Pick<Content, 'children_count' | 'completed_children_count' | 'dropped_children_count'>
): string | null {
    const total = series.children_count ?? 0
    if (total <= 0) return null
    const dropped = series.dropped_children_count ?? 0
    const label = `${series.completed_children_count ?? 0}/${total} read`
    return dropped ? `${label} · ${dropped} dropped` : label
}
