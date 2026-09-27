import type { BookSpread } from './bookSettings'

export interface GeometryInput {
    /** The viewport's box, px */
    width: number
    height: number
    fontPx: number
    /** The text width setting, em */
    textWidth: number
    spread: BookSpread
}

/** All integer px: fractional columns drift by a pixel every few dozen. */
export interface PagedGeometry {
    colW: number
    gap: number
    spread: 1 | 2
    frameW: number
    pageH: number
    padTop: number
    colPitch: number
    screenPitch: number
}

const MIN_COLUMN_EM = 12
const AUTO_SPREAD = 0.75

/** The text width is the widest a column gets; leftover width is margin. */
export function computeGeometry(input: GeometryInput): PagedGeometry {
    const { width, height, fontPx } = input
    const maxCol = Math.round(input.textWidth * fontPx)
    const minCol = Math.round(MIN_COLUMN_EM * fontPx)
    const side = width < 600 ? 16 : 32
    const gap = Math.max(2 * side, Math.round(3 * fontPx))
    const avail = Math.max(1, Math.floor(width) - 2 * side)
    const twoCol = Math.floor((avail - gap) / 2)
    const twoFit =
        input.spread === '2'
            ? twoCol >= minCol
            : input.spread === 'auto' && twoCol >= Math.round(AUTO_SPREAD * maxCol)
    const spread = twoFit ? 2 : 1
    const colW = Math.max(1, Math.min(maxCol, spread === 2 ? twoCol : avail))
    const padTop = side
    const colPitch = colW + gap
    return {
        colW,
        gap,
        spread,
        frameW: spread * colW + (spread - 1) * gap,
        pageH: Math.max(1, Math.floor(height) - padTop),
        padTop,
        colPitch,
        screenPitch: spread * colPitch,
    }
}
