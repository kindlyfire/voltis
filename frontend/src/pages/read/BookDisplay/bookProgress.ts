import type { SpineItem } from '@/utils/api/types'

export interface FlowWeights {
    weights: number[]
    total: number
}

/** A book scanned before word counting reports 0 everywhere and is never
 * backfilled, so fall back to equal per-document weights rather than dividing
 * by zero. The backend floors genuinely textless documents itself. */
export function flowWeights(spine: SpineItem[]): FlowWeights {
    const counted = spine.some(item => item.linear && item.words > 0)
    const weights = spine.map(item => (!item.linear ? 0 : counted ? item.words : 1))
    return { weights, total: weights.reduce((sum, w) => sum + w, 0) }
}

function clamp01(value: number): number {
    return Math.min(1, Math.max(0, value))
}

/** The backend's word counts scaled by the client's own character-based
 * intra-document ratio: one measurement path, and neither side has to count
 * text the way the other does. */
export function progressPercent(
    { weights, total }: FlowWeights,
    spineIndex: number,
    intraDocTextFraction: number
): number {
    if (!total || spineIndex < 0 || spineIndex >= weights.length) return 0
    let before = 0
    for (let i = 0; i < spineIndex; i++) before += weights[i]!
    const value = (before + weights[spineIndex]! * clamp01(intraDocTextFraction)) / total
    return Math.round(clamp01(value) * 1000) / 10
}
