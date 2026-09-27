import { describe, expect, it } from 'vitest'
import type { Content } from '@/utils/api/types'
import { nextVolume } from './useNextVolume'

function book(id: string, order: number | null, extra: Partial<Content> = {}): Content {
    return { id, order, type: 'book', valid: true, ...extra } as Content
}

const current = book('b2', 2)

describe('next volume', () => {
    it.each([
        [
            'takes the smallest later order, whatever the list order',
            [book('b5', 5), current, book('b1', 1), book('b3', 3), book('bn', null)],
            'b3',
        ],
        ['bridges a gap in the orders', [current, book('b4', 4), book('b7', 7)], 'b4'],
        [
            'skips invalid items and other content types',
            [
                current,
                book('bad', 3, { valid: false }),
                book('comic', 3, { type: 'comic' }),
                book('b4', 4),
            ],
            'b4',
        ],
        ['offers nothing after the last volume', [book('b1', 1), current], null],
    ])('%s', (_name, siblings, expected) => {
        expect(nextVolume(current, siblings)?.id ?? null).toBe(expected)
    })

    it('offers nothing while the current order is unknown', () => {
        const unknown = book('b', null)
        expect(nextVolume(unknown, [unknown, book('b1', 1)])).toBeNull()
    })
})
