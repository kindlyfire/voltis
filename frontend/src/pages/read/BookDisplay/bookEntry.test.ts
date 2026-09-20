import { describe, expect, it } from 'vitest'
import type { SpineItem } from '@/utils/api/types'
import { chooseEntryPage, isBookLocator, parseBookEntry } from './bookEntry'
import { flowWeights, progressPercent } from './bookProgress'

describe('entry parsing', () => {
    it('validates query values instead of casting them', () => {
        expect(parseBookEntry({ ch: 'a.xhtml', frag: 'f1', page: 'resume' })).toEqual({
            ch: 'a.xhtml',
            frag: 'f1',
        })
        expect(parseBookEntry({ ch: ['a.xhtml', 'b.xhtml'], page: '3' })).toEqual({
            ch: 'a.xhtml',
            frag: null,
        })
        expect(parseBookEntry({ ch: '', frag: null })).toEqual({ ch: null, frag: null })
        expect(parseBookEntry({ ch: 42 })).toEqual({ ch: null, frag: null })
    })
})

describe('entry precedence', () => {
    it('lets the locator win on the same page and the URL win on another', () => {
        expect(chooseEntryPage(2, 2)).toEqual({ pageIndex: 2, useLocator: true })
        expect(chooseEntryPage(2, 5)).toEqual({ pageIndex: 2, useLocator: false })
        expect(chooseEntryPage(null, 5)).toEqual({ pageIndex: 5, useLocator: true })
        expect(chooseEntryPage(3, null)).toEqual({ pageIndex: 3, useLocator: false })
        expect(chooseEntryPage(null, null)).toEqual({ pageIndex: 0, useLocator: false })
    })
})

describe('locator validation', () => {
    it('accepts a zero offset and rejects malformed values', () => {
        expect(isBookLocator({ version: 1, href: 'a.xhtml', textOffset: 0 })).toBe(true)
        expect(isBookLocator({ version: 1, href: 'a.xhtml', textOffset: 5, anchorId: 'x' })).toBe(
            true
        )
        expect(isBookLocator({ version: 2, href: 'a.xhtml', textOffset: 0 })).toBe(false)
        expect(isBookLocator({ version: 1, href: '', textOffset: 0 })).toBe(false)
        expect(isBookLocator({ version: 1, href: 'a.xhtml' })).toBe(false)
        expect(isBookLocator(null)).toBe(false)
        expect(isBookLocator(undefined)).toBe(false)
    })
})

function spine(items: Array<Partial<SpineItem> & { href: string }>): SpineItem[] {
    return items.map(item => ({ title: item.href, linear: true, words: 0, ...item }))
}

describe('percent', () => {
    it('scales backend word counts by the intra-document character fraction', () => {
        const weights = flowWeights(
            spine([
                { href: 'a', words: 1000 },
                { href: 'b', words: 3000 },
            ])
        )
        expect(progressPercent(weights, 0, 0)).toBe(0)
        expect(progressPercent(weights, 1, 0)).toBe(25)
        expect(progressPercent(weights, 1, 0.5)).toBe(62.5)
        expect(progressPercent(weights, 1, 1)).toBe(100)
    })

    // Every book scanned before word counting reports 0 and is never
    // backfilled, so this is the path most existing books take.
    it('falls back to equal weights when nothing was counted', () => {
        const weights = flowWeights(spine([{ href: 'a' }, { href: 'b' }, { href: 'c' }]))
        expect(weights.weights).toEqual([1, 1, 1])
        expect(weights.total).toBe(3)
        expect(progressPercent(weights, 0, 0)).toBe(0)
        expect(progressPercent(weights, 1, 0)).toBeCloseTo(33.3, 1)
        expect(progressPercent(weights, 1, 0.5)).toBe(50)
        expect(progressPercent(weights, 2, 1)).toBe(100)
    })

    it('still falls back when only a non-linear document was counted', () => {
        const weights = flowWeights(
            spine([{ href: 'a' }, { href: 'notes', words: 400, linear: false }, { href: 'b' }])
        )
        expect(weights.weights).toEqual([1, 0, 1])
        expect(progressPercent(weights, 2, 0)).toBe(50)
    })

    it('leaves non-linear documents out of the denominator', () => {
        const weights = flowWeights(
            spine([
                { href: 'a', words: 500 },
                { href: 'notes', words: 500, linear: false },
            ])
        )
        expect(weights.total).toBe(500)
        expect(progressPercent(weights, 0, 1)).toBe(100)
    })

    it('takes the backend counts verbatim, floor included', () => {
        const weights = flowWeights(
            spine([
                { href: 'plate', words: 10 },
                { href: 'a', words: 990 },
            ])
        )
        expect(weights.weights).toEqual([10, 990])
        expect(progressPercent(weights, 1, 0)).toBe(1)
    })

    it('does not inflate a short document', () => {
        const weights = flowWeights(
            spine([
                { href: 'a', words: 3 },
                { href: 'b', words: 100 },
            ])
        )
        expect(weights.weights).toEqual([3, 100])
        expect(progressPercent(weights, 1, 0)).toBeCloseTo(2.9, 1)
    })
})
