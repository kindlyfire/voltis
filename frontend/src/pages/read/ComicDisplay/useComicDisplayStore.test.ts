import { describe, expect, it } from 'vitest'
import { detectMode, pageStyle } from './useComicDisplayStore'

describe('pageStyle', () => {
    it('is empty for an unknown size', () => {
        expect(pageStyle({ width: 0, height: 1200 }, 80)).toEqual({})
        expect(pageStyle({ width: 800, height: 0 }, 80)).toEqual({})
    })

    it('sizes a known page', () => {
        expect(pageStyle({ width: 800, height: 1200 }, 80)).toEqual({
            width: 'min(80%, 800px)',
            aspectRatio: '800 / 1200',
        })
    })
})

describe('detectMode', () => {
    it('is paged without known sizes', () => {
        expect(detectMode([])).toBe('paged')
        expect(detectMode([{ width: 0, height: 0 }])).toBe('paged')
    })

    it('averages only known sizes', () => {
        const strip = { width: 800, height: 3000 }
        expect(detectMode([strip, { width: 0, height: 0 }])).toBe('longstrip')
        expect(detectMode([strip, { width: 800, height: 1000 }])).toBe('longstrip')
        expect(
            detectMode([
                { width: 800, height: 1200 },
                { width: 0, height: 0 },
            ])
        ).toBe('paged')
    })
})
