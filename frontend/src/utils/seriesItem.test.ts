import { describe, expect, it } from 'vitest'
import type { Content } from '@/utils/api/types'
import { readerTitle, splitItemTitle, type SeriesInfo } from './seriesItem'

function series(
    title = 'Ember Saga',
    type = 'book_series',
    meta: { kind?: string; alt_titles?: string[] } = {}
) {
    return { title, type, meta } as unknown as SeriesInfo
}

function item(order_parts: (number | null)[], title: string) {
    return { title, order_parts } as unknown as Content
}

const harbor = (kind?: string) => series('Harbor Lights', 'comic_series', { kind })

describe('series items', () => {
    it.each([
        ['Ember Saga, Vol. 01', 'Volume 1', series(), [1], null, true],
        ['Ember Saga, Vol. 3: The Long Road', 'Volume 3', series(), [3], 'The Long Road', true],
        ['Ember Saga, Vol. 3 – The Long Road', 'Volume 3', series(), [3], 'The Long Road', true],
        ['Ember Saga, Vol. 1-3', 'Volume 1', series(), [1], null, true],
        ['Ember Saga v 2', 'Volume 2', series(), [2], null, true],
        ['Ember Saga 3', 'Volume 3', series(), [3], null, true],
        ['Ember Saga 3: The Long Road', 'Volume 3', series(), [3], 'The Long Road', true],
        ['Ember Saga: 7 Days', 'Volume 1', series(), [1], '7 Days', false],
        ['Ember Saga, Vol. 2: 7 Days', 'Volume 2', series(), [2], '7 Days', true],
        ['1984', 'Volume 3', series(), [3], null, false],
        ['v3', 'Volume 3', series(), [3], null, true],
        ['Ember Saga, Vol. 3.5', 'Volume 3.5', series(), [3.5], null, true],
        ['Ember Saga: Extras', null, series(), [-1], 'Extras', false],
        [
            'Ember Saga, Vol. 3: The Long Road',
            null,
            series(),
            [null],
            'Vol. 3: The Long Road',
            false,
        ],
        ['EMBER  SAGA – The Long Road', null, series(), [null], 'The Long Road', false],
        [
            'Saga of Embers, Vol. 3: The Long Road',
            'Volume 3',
            series('Ember Saga', 'book_series', { alt_titles: ['Saga of Embers'] }),
            [3],
            'The Long Road',
            true,
        ],
        ['Embers of Dusk', null, series('Ember'), [null], 'Embers of Dusk', false],
        [
            "The Tinker's Road: Short Stories",
            null,
            series('The Tinker’s Road'),
            [100000],
            'Short Stories',
            false,
        ],
        ['The Return', 'Chapter 12', harbor('manga'), [null, 12], 'The Return', false],
        ['Vol. 2 Ch. 5', 'Vol. 2 · Ch. 5', harbor('manga'), [2, 5], null, true],
        ['Vol. 2 Ch. 5', 'Vol. 2 · #5', harbor('comic'), [2, 5], null, true],
        ['#12', '#12', harbor('comic'), [null, 12], null, true],
        ['Vol. 3', 'Volume 3', harbor(), [3, null], null, true],
    ] as const)('%s → %s', (title, label, s, parts, stripped, removedNumber) => {
        expect(splitItemTitle(item([...parts], title), s)).toEqual({
            label,
            stripped,
            removedNumber,
        })
    })

    it('titles the reader', () => {
        expect(readerTitle(item([null], 'Ember Saga'), series())).toBe('Ember Saga')
    })
})
