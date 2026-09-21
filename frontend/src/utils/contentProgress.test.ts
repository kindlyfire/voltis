import { describe, expect, it } from 'vitest'
import type { Content, ReadingProgress, ReadingStatus } from '@/utils/api/types'
import { contentProgress, type ContentProgress } from './contentProgress'

/** `pages` undefined mirrors a list response, which omits `file_data`. */
function comic(progress: ReadingProgress, pages?: number, status: ReadingStatus = 'reading') {
    return {
        type: 'comic',
        file_data: pages == null ? undefined : { pages: Array.from({ length: pages }, () => []) },
        user_data: { progress, status },
    } as unknown as Content
}

function book(percent: number | undefined, status: ReadingStatus = 'reading') {
    return {
        type: 'book',
        user_data: { progress: { progress_percent: percent }, status },
    } as unknown as Content
}

function series(total: number, unread: number, status: ReadingStatus | null = null) {
    return {
        type: 'comic_series',
        children_count: total,
        unread_children_count: unread,
        user_data: status && { progress: {}, status },
    } as unknown as Content
}

const CASES: Array<[name: string, content: Content, expected: ContentProgress | null]> = [
    ['comic merely opened', comic({ current_page: 0, progress_percent: 0.7 }, 140), null],
    ['comic merely opened, no file_data', comic({ current_page: 0, progress_percent: 0.7 }), null],
    ['comic without current_page', comic({}, 140), null],
    ['comic pre-change progress, no file_data', comic({ current_page: 12 }), null],
    [
        'comic mid-read',
        comic({ current_page: 12, progress_percent: 50 }, 140),
        { fraction: 13 / 140, label: 'Page 13 / 140' },
    ],
    [
        'comic mid-read, no file_data',
        comic({ current_page: 12, progress_percent: 9.3 }),
        { fraction: 0.093, label: '9%' },
    ],
    ['comic on the last page', comic({ current_page: 139 }, 140), null],
    ['comic completed', comic({ current_page: 12 }, 140, 'completed'), null],
    ['book untouched', book(0), null],
    ['book without a percent', book(undefined), null],
    ['book barely started', book(0.4), { fraction: 0.004, label: '1%' }],
    ['book nearly done', book(99.6), { fraction: 0.996, label: '99%' }],
    ['book finished', book(100), null],
    ['book dropped', book(40, 'dropped'), null],
    ['series partially read', series(10, 4), { fraction: 0.6, label: '6 / 10 chapters' }],
    ['series with nothing read', series(10, 10), null],
    ['series fully read', series(10, 0), null],
    ['series completed', series(10, 4, 'completed'), null],
]

describe('contentProgress', () => {
    it.each(CASES)('%s', (_name, content, expected) => {
        const result = contentProgress(content)
        if (!expected) {
            expect(result).toBeNull()
            return
        }
        expect(result?.label).toBe(expected.label)
        expect(result?.fraction).toBeCloseTo(expected.fraction, 6)
    })
})
