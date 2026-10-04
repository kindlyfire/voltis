import type { FitMode, PageDimensions } from './types'

/** How much bigger half a wide page must get before zooming into it is worth a scroll. */
const ZOOM_MIN_GAIN = 1.25

// A width or height of 0 means the size is unknown.
export const sized = (p: PageDimensions) => p.width > 0 && p.height > 0

function isWide(p: PageDimensions): boolean {
    return sized(p) && p.width > p.height
}

/** Groups pages into screens of ascending page indices. In double mode the cover and wide pages
 * stand alone (unless `shift` pairs the cover), and pairing restarts after a wide page. Unknown
 * sizes count as portrait. */
export function buildSpreads(
    pages: readonly PageDimensions[],
    o: { double: boolean; shift: boolean }
): number[][] {
    if (!o.double) return pages.map((_, i) => [i])
    const pairable = (i: number) => i < pages.length && !isWide(pages[i]!) && (i > 0 || o.shift)
    const spreads: number[][] = []
    for (let i = 0; i < pages.length;) {
        if (pairable(i) && pairable(i + 1)) {
            spreads.push([i, i + 1])
            i += 2
        } else {
            spreads.push([i++])
        }
    }
    return spreads
}

/** Maps each page to the index of the spread holding it. */
export function spreadOfPages(spreads: number[][], pageCount: number): number[] {
    const out = new Array<number>(pageCount)
    spreads.forEach((s, i) => s.forEach(page => (out[page] = i)))
    return out
}

/** Sizes a spread in CSS px, all pages sharing one height. Null when a page size is unknown. */
export function layoutSpread(
    pages: readonly PageDimensions[],
    viewport: { width: number; height: number },
    o: { fit: FitMode; zoomWide: boolean }
): { pages: { width: number; height: number }[]; width: number; height: number } | null {
    if (!pages.length || !pages.every(sized)) return null
    const { width: vw, height: vh } = viewport
    const aspect = pages.reduce((sum, p) => sum + p.width / p.height, 0)

    let h = o.fit === 'height' ? vh : o.fit === 'width' ? vw / aspect : Math.min(vh, vw / aspect)
    // Wide zoom: one wide page shown half at a time, overflowing horizontally.
    if (o.fit === 'screen' && o.zoomWide && pages.length === 1 && isWide(pages[0]!)) {
        const half = Math.min(vh, vw / (aspect / 2))
        if (half >= ZOOM_MIN_GAIN * h) h = half
    }

    return {
        pages: pages.map(p => ({ width: (h * p.width) / p.height, height: h })),
        width: h * aspect,
        height: h,
    }
}

/** The next scroll target along one axis toward the reading-order end (`toEnd`) or start, or
 * null when already at that edge. Snaps to the edge when the rest fits in about one step. */
export function scrollStep(
    pos: number,
    max: number,
    viewport: number,
    toEnd: boolean
): number | null {
    const step = viewport - Math.max(32, 0.1 * viewport)
    const remaining = toEnd ? max - pos : pos
    if (remaining <= 2) return null
    if (remaining <= viewport) return toEnd ? max : 0
    return toEnd ? pos + step : pos - step
}
