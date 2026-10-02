import { describe, expect, it } from 'vitest'
import type { Content } from '@/utils/api/types'
import type { Siblings } from '../useSiblings'
import { nextVolume } from './useNextVolume'

function book(id: string, extra: Partial<Content> = {}): Content {
    return { id, type: 'book', valid: true, ...extra } as Content
}

const siblings = (items: Content[], index: number, status: Siblings['status'] = 'ready') =>
    ({ status, items, index }) as Siblings

describe('next volume', () => {
    it.each([
        ['takes the one after the current', [book('b1'), book('b2'), book('b3')], 'b3'],
        [
            'skips invalid items and other content types',
            [
                book('b1'),
                book('b2'),
                book('bad', { valid: false }),
                book('comic', { type: 'comic' }),
                book('b4'),
            ],
            'b4',
        ],
        ['offers nothing after the last volume', [book('b1'), book('b2')], null],
    ])('%s', (_name, items, expected) => {
        expect(nextVolume(siblings(items, 1))?.id ?? null).toBe(expected)
    })

    it('offers nothing until the siblings are known', () => {
        expect(nextVolume(siblings([book('b1'), book('b2')], 0, 'loading'))).toBeNull()
        expect(nextVolume(siblings([], -1))).toBeNull()
    })
})
