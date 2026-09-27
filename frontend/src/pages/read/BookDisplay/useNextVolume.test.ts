import { describe, expect, it } from 'vitest'
import type { Content } from '@/utils/api/types'
import { nextVolume } from './useNextVolume'

function book(id: string, order: number | null, extra: Partial<Content> = {}): Content {
    return { id, order, type: 'book', valid: true, ...extra } as Content
}

describe('next volume', () => {
    it('takes the smallest order after the current one, whatever the list order', () => {
        const current = book('b2', 2)
        const siblings = [book('b5', 5), current, book('b1', 1), book('b3', 3), book('bn', null)]
        expect(nextVolume(current, siblings)?.id).toBe('b3')
    })

    it('bridges a gap in the orders', () => {
        const current = book('b1', 1)
        expect(nextVolume(current, [current, book('b4', 4), book('b7', 7)])?.id).toBe('b4')
    })

    it('offers nothing while the current order is unknown', () => {
        const current = book('b', null)
        expect(nextVolume(current, [current, book('b1', 1)])).toBeNull()
    })

    it('skips invalid items and other content types', () => {
        const current = book('b1', 1)
        const siblings = [
            current,
            book('bad', 2, { valid: false }),
            book('comic', 3, { type: 'comic' }),
            book('b4', 4),
        ]
        expect(nextVolume(current, siblings)?.id).toBe('b4')
    })

    it('offers nothing after the last volume', () => {
        const current = book('b2', 2)
        expect(nextVolume(current, [book('b1', 1), current])).toBeNull()
    })
})
