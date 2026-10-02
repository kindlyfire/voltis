import { describe, expect, it } from 'vitest'
import type { Content, ReadingProgress, ReadingStatus } from '@/utils/api/types'
import { childNoun, contentProgress, seriesPosition, type ContentProgress } from './contentProgress'

/** `pages` undefined mirrors a list response, which omits `file_data`. */
function comic(progress: ReadingProgress, pages?: number, status: ReadingStatus = 'reading') {
    return {
        type: 'comic',
        file_data:
            pages == null
                ? undefined
                : { pages: Array.from({ length: pages }, (_, i) => [`${i}.jpg`]) },
        user_data: { progress, status },
    } as unknown as Content
}

function book(percent: number | undefined, status: ReadingStatus = 'reading') {
    return {
        type: 'book',
        user_data: { progress: { progress_percent: percent }, status },
    } as unknown as Content
}

function series(total: number, unread: number, status: ReadingStatus | null = null, dropped = 0) {
    return {
        type: 'comic_series',
        children_count: total,
        unread_children_count: unread,
        completed_children_count: total - unread - dropped,
        dropped_children_count: dropped,
        user_data: status && { progress: {}, status },
    } as unknown as Content
}

const CASES: Array<[name: string, content: Content, expected: ContentProgress | null]> = [
    [
        'comic saved on its first page',
        comic({ current_page: 0, progress_percent: 0 }, 140),
        { fraction: 0, label: 'Page 1 / 140' },
    ],
    ['comic one-page, read', comic({ current_page: 0 }, 1), { fraction: 0, label: 'Page 1 / 1' }],
    [
        'comic first page, no file_data',
        comic({ current_page: 0, progress_percent: 0 }),
        { fraction: 0, label: '0%' },
    ],
    ['comic without current_page', comic({}, 140), null],
    ['comic pre-change progress, no file_data', comic({ current_page: 12 }), null],
    [
        'comic mid-read',
        comic({ current_page: 12, progress_percent: 50 }, 140),
        { fraction: 12 / 140, label: 'Page 13 / 140' },
    ],
    [
        'comic mid-read, no file_data',
        comic({ current_page: 12, progress_percent: 9.3 }),
        { fraction: 0.093, label: '9%' },
    ],
    [
        'comic on the last page',
        comic({ current_page: 139 }, 140),
        { fraction: 139 / 140, label: 'Page 140 / 140' },
    ],
    ['comic completed', comic({ current_page: 12 }, 140, 'completed'), null],
    [
        'comic saved past a rescan that removed pages',
        comic({ current_page: 40 }, 10),
        { fraction: 0.9, label: 'Page 10 / 10' },
    ],
    ['book saved at its start', book(0), { fraction: 0, label: '0%' }],
    ['book without a percent', book(undefined), null],
    ['book barely started', book(0.4), { fraction: 0.004, label: '1%' }],
    ['book nearly done', book(99.6), { fraction: 0.996, label: '99%' }],
    ['book at its end, not completed', book(100), { fraction: 1, label: '100%' }],
    ['book dropped', book(40, 'dropped'), null],
    ['series partially read', series(10, 4), { fraction: 0.6, label: '6/10 read' }],
    [
        'series partially read and dropped',
        series(12, 1, null, 1),
        { fraction: 11 / 12, label: '10/12 read · 1 dropped' },
    ],
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

describe('seriesPosition', () => {
    it('counts read children', () => {
        expect(seriesPosition(series(10, 7))).toEqual({ read: 3, total: 10 })
    })

    it('names children by type', () => {
        expect(childNoun('book_series', 2)).toBe('volumes')
        expect(childNoun('comic_series', 1)).toBe('chapter')
    })

    it('is null without children', () => {
        expect(seriesPosition(series(0, 0))).toBeNull()
        expect(seriesPosition({ type: 'comic_series' } as Content)).toBeNull()
    })
})
