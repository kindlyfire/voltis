import { describe, expect, it } from 'vitest'
import { detectDirection } from './direction'
import { buildSpreads, layoutSpread, scrollStep, spreadOfPages } from './pagedLayout'
import type { PageDimensions } from './types'

const P: PageDimensions = { width: 800, height: 1200 }
const W: PageDimensions = { width: 1600, height: 1200 }
const U: PageDimensions = { width: 0, height: 0 }

describe('buildSpreads', () => {
    it.each([
        ['single mode', [P, P, P], false, false, [[0], [1], [2]]],
        ['cover alone, then pairs', [P, P, P, P, P], true, false, [[0], [1, 2], [3, 4]]],
        ['odd tail alone', [P, P, P, P], true, false, [[0], [1, 2], [3]]],
        [
            'wide page alone, leaving a single and restarting pairs',
            [P, P, W, P, P, P],
            true,
            false,
            [[0], [1], [2], [3, 4], [5]],
        ],
        ['shift pairs the cover', [P, P, P, W], true, true, [[0, 1], [2], [3]]],
        ['unknown sizes as portrait', [U, U, U], true, false, [[0], [1, 2]]],
    ])('%s', (_name, pages, double, shift, expected) => {
        const spreads = buildSpreads(pages, { double, shift })
        expect(spreads).toEqual(expected)
        const of = spreadOfPages(spreads, pages.length)
        expected.forEach((s, i) => s.forEach(page => expect(of[page]).toBe(i)))
    })
})

describe('layoutSpread', () => {
    const portrait = { width: 400, height: 800 }
    const landscape = { width: 1600, height: 900 }
    it.each([
        // Zoomed: twice the viewport width, so each scroll edge shows one half.
        ['portrait viewport zooms a wide page', [W], portrait, 'screen', true, 600, 800],
        ['landscape viewport gains too little', [W], landscape, 'screen', true, 900, 1200],
        ['gain just below 1.25', [W], { width: 600, height: 562 }, 'screen', true, 450, 600],
        ['gain of exactly 1.25', [W], { width: 600, height: 562.5 }, 'screen', true, 562.5, 750],
        ['zoomWide off', [W], portrait, 'screen', false, 300, 400],
        ['fit width never zooms', [W], portrait, 'width', true, 300, 400],
        ['fit height never zooms', [W], portrait, 'height', true, 800, 1066.67],
        ['two pages share one height', [P, P], landscape, 'screen', true, 900, 1200],
    ] as const)('%s', (_name, pages, viewport, fit, zoomWide, height, width) => {
        const l = layoutSpread(pages, viewport, { fit, zoomWide })!
        expect(l.height).toBeCloseTo(height)
        expect(l.width).toBeCloseTo(width)
        expect(l.pages.reduce((sum, p) => sum + p.width, 0)).toBeCloseTo(width)
    })

    it('is null for an unknown size', () => {
        expect(
            layoutSpread([P, U], { width: 1000, height: 1000 }, { fit: 'screen', zoomWide: true })
        ).toBeNull()
    })
})

describe('scrollStep', () => {
    it.each([
        ['steps by viewport minus overlap', 0, 3000, 1000, true, 900],
        ['steps back', 2000, 3000, 1000, false, 1100],
        ['overlap is at least 32px', 0, 3000, 200, true, 168],
        ['snaps to the end', 2100, 3000, 1000, true, 3000],
        ['snaps to the start', 900, 3000, 1000, false, 0],
        ['null at the end', 2999, 3000, 1000, true, null],
        ['null at the start', 0, 3000, 1000, false, null],
        // A zoomed wide page: max ≤ half width ≤ viewport.
        ['wide page reaches the far half from the start', 0, 400, 400, true, 400],
        ['wide page reaches the far half mid-pan', 150, 300, 400, true, 300],
        ['wide page returns to the first half', 250, 300, 400, false, 0],
    ])('%s', (_name, pos, max, viewport, toEnd, expected) => {
        expect(scrollStep(pos, max, viewport, toEnd)).toBe(expected)
    })
})

describe('detectDirection', () => {
    it.each([
        ['comic RTL tag', { manga: 'YesAndRightToLeft' }, { manga: 'No' }, 'rtl'],
        ['comic No beats series kind', { manga: 'no' }, { kind: 'manga' }, 'ltr'],
        [
            'comic Yes falls through to series',
            { manga: 'Yes' },
            { manga: 'yesandrighttoleft' },
            'rtl',
        ],
        ['series No beats kind', { manga: 'Unknown' }, { manga: 'No', kind: 'manga' }, 'ltr'],
        ['series kind manga', {}, { manga: 'Yes', kind: 'manga' }, 'rtl'],
        ['other kind', {}, { kind: 'comic' }, 'ltr'],
        ['nothing known', undefined, null, 'ltr'],
    ] as const)('%s', (_name, comic, series, expected) => {
        expect(detectDirection(comic, series)).toBe(expected)
    })
})
