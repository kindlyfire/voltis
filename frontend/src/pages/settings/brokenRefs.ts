import {
    READING_STATUS_LABELS,
    type BrokenRefGroup,
    type BrokenUserToContent,
} from '@/utils/api/types'

/** Whether a group holds children of a series, rather than one top-level item. */
export const isSeriesGroup = (g: BrokenRefGroup) =>
    g.items.some(i => i.uri !== g.uri) || g.list_entries.some(e => e.uri !== g.uri)

/** A row's status, rating and notes, in short. */
export function summarize(item: BrokenUserToContent): string {
    const parts: string[] = []
    if (item.status) parts.push(READING_STATUS_LABELS[item.status])
    if (item.rating != null) parts.push(`rating: ${item.rating}`)
    if (item.notes) parts.push('has notes')
    return parts.join(', ') || '—'
}
