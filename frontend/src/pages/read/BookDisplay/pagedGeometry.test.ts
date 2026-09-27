import { describe, expect, it } from 'vitest'
import { computeGeometry, type GeometryInput } from './pagedGeometry'

const DEFAULTS: GeometryInput = {
    width: 1280,
    height: 800,
    fontPx: 17.6,
    textWidth: 45,
    spread: 'auto',
}

const at = (input: Partial<GeometryInput>) => computeGeometry({ ...DEFAULTS, ...input })

describe('paged geometry', () => {
    it('switches auto to two columns once each is 0.75 of the text width', () => {
        expect(at({ width: 1280 })).toMatchObject({ spread: 1, colW: 792 })
        expect(at({ width: 1366 })).toMatchObject({ spread: 2, colW: 619 })
        expect(at({ width: 1920 })).toMatchObject({ spread: 2, colW: 792 })
    })

    it('keeps forced two columns down to 12em, then falls back to one', () => {
        expect(at({ width: 900, spread: '2' }).spread).toBe(2)
        expect(at({ width: 500, spread: '2' })).toMatchObject({ spread: 1, colW: 468 })
        expect(at({ width: 1920, spread: '1' }).spread).toBe(1)
    })

    it('works in integer px, with the gap at least both side margins', () => {
        for (const width of [390, 777, 1280, 1366.5, 2560]) {
            for (const spread of ['1', '2', 'auto'] as const) {
                const g = at({ width, height: 700.4, fontPx: 16.3, spread })
                for (const value of Object.values(g)) expect(Number.isInteger(value)).toBe(true)
                expect(g.gap).toBeGreaterThanOrEqual(width < 600 ? 32 : 64)
                expect(g.frameW).toBeLessThanOrEqual(width)
                expect(g.screenPitch).toBe(g.spread * (g.colW + g.gap))
            }
        }
    })

    it('fills the viewport height below the top padding', () => {
        expect(at({ height: 760.7 })).toMatchObject({ padTop: 32, pageH: 728 })
        expect(at({ width: 390, height: 700 })).toMatchObject({ padTop: 16, pageH: 684 })
    })
})
