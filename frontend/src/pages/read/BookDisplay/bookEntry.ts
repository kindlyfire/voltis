import type { BookLocator } from '@/utils/api/types'

export interface BookEntry {
    ch: string | null
    frag: string | null
}

function queryString(value: unknown): string | null {
    if (typeof value === 'string' && value !== '') return value
    if (Array.isArray(value)) return queryString(value[0])
    return null
}

export function parseBookEntry(query: Record<string, unknown>): BookEntry {
    return { ch: queryString(query.ch), frag: queryString(query.frag) }
}

export function entryKey(entry: BookEntry): string {
    return `${entry.ch ?? ''}#${entry.frag ?? ''}`
}

/** Same chapter → the locator wins (resume mid-page); different chapter → the URL
 * wins (deliberate deep link); neither → flow start. */
export function chooseEntryChapter(urlChapter: number | null, locatorChapter: number | null) {
    if (urlChapter != null && locatorChapter != null) {
        return { chapterIndex: urlChapter, useLocator: urlChapter === locatorChapter }
    }
    if (urlChapter != null) return { chapterIndex: urlChapter, useLocator: false }
    if (locatorChapter != null) return { chapterIndex: locatorChapter, useLocator: true }
    return { chapterIndex: 0, useLocator: false }
}

export function isBookLocator(value: unknown): value is BookLocator {
    if (typeof value !== 'object' || value === null) return false
    const v = value as Record<string, unknown>
    return (
        v.version === 1 &&
        typeof v.href === 'string' &&
        v.href !== '' &&
        typeof v.textOffset === 'number' &&
        Number.isFinite(v.textOffset) &&
        (v.anchorId === undefined || typeof v.anchorId === 'string')
    )
}
