import type { Content } from '@/utils/api/types'

/** A series as far as labeling its items goes; continue series have an empty `meta`. */
export type SeriesInfo = Pick<Content, 'type' | 'title' | 'meta'>
type Item = Pick<Content, 'title' | 'order_parts'>

const DASHES = '‐‑‒–—―'
const MARKS = `:,·\\-${DASHES}`
const SEP = `[\\s${MARKS}]`
// Mirrors the backend's `bookVolumeRange`.
const NUM = String.raw`\d+(?:\.\d+)?(?:\s*[-–~]\s*\d+(?:\.\d+)?)?`

const SEP_RE = new RegExp(SEP, 'u')
const LEADING_SEP = new RegExp(`^${SEP}+`, 'u')
const BARE_NUMBER = new RegExp(`^${NUM}(?=$|\\s*[${MARKS}])`, 'u')
const MARKER = new RegExp(
    String.raw`^(?:(?:volume|vol|v|chapter|ch)\.?|#)\s*${NUM}(?![\p{L}\p{N}])`,
    'iu'
)
const ONLY_NUM = new RegExp(`^${NUM}$`, 'u')
const HAS_TEXT = /[\p{L}\p{N}]/u
const WS = /\s/u

/** Drops sentinels such as calibre's 100000 for specials. */
function plausible(n: number | null | undefined): n is number {
    return typeof n === 'number' && Number.isFinite(n) && n >= 0 && n < 10000
}

/** "Volume 3", "Chapter 12", "Vol. 2 · #5"; null when the series has no numbering. */
function numberLabel(parts: (number | null)[], series: SeriesInfo): string | null {
    const vol = plausible(parts[0]) ? parts[0] : null
    if (series.type === 'book_series') return vol == null ? null : `Volume ${vol}`
    if (series.type !== 'comic_series') return null
    const ch = plausible(parts[1]) ? parts[1] : null
    const western = series.meta?.kind === 'comic'
    if (vol != null && ch != null) return `Vol. ${vol} · ${western ? '#' : 'Ch. '}${ch}`
    if (ch != null) return western ? `#${ch}` : `Chapter ${ch}`
    if (vol != null) return western ? `Vol. ${vol}` : `Volume ${vol}`
    return null
}

function fold(ch: string): string {
    if ('’‘‛′'.includes(ch)) return "'"
    if ('“”″'.includes(ch)) return '"'
    if (DASHES.includes(ch)) return '-'
    return ch.toLowerCase()
}

/** The rest of `title` after `prefix`, or null when it doesn't start with it at a separator. */
function afterPrefix(title: string[], prefix: string[]): string | null {
    let i = 0
    let j = 0
    while (j < prefix.length) {
        if (i >= title.length) return null
        const ws = WS.test(prefix[j]!)
        if (ws !== WS.test(title[i]!)) return null
        if (ws) {
            while (j < prefix.length && WS.test(prefix[j]!)) j++
            while (i < title.length && WS.test(title[i]!)) i++
            continue
        }
        if (fold(title[i]!) !== fold(prefix[j]!)) return null
        i++
        j++
    }
    if (i < title.length && !SEP_RE.test(title[i]!)) return null
    return title.slice(i).join('')
}

interface SplitTitle {
    label: string | null
    /** The item title without the series name and, when labeled, its numbering. */
    stripped: string | null
    removedNumber: boolean
}

/** An item's number label and its title shortened against the series. */
export function splitItemTitle(c: Item, series: SeriesInfo | null | undefined): SplitTitle {
    if (!series) return { label: null, stripped: null, removedNumber: false }
    const label = numberLabel(c.order_parts ?? [], series)
    const chars = Array.from(c.title ?? '')
    const after = [series.title, ...(series.meta?.alt_titles ?? [])]
        .map(t => Array.from((t ?? '').trim()))
        .filter(t => t.length)
        .sort((a, b) => b.length - a.length)
        .map(p => afterPrefix(chars, p))
        .find(r => r != null)

    let rest = after != null ? after.replace(LEADING_SEP, '') : (c.title ?? '')
    const before = rest
    if (label != null) {
        if (after != null) rest = rest.replace(BARE_NUMBER, '').replace(LEADING_SEP, '')
        while (MARKER.test(rest)) rest = rest.replace(MARKER, '').replace(LEADING_SEP, '')
    }
    const removedNumber = rest !== before

    rest = rest.trim()
    const stripped = HAS_TEXT.test(rest) && !ONLY_NUM.test(rest) ? rest : null
    return { label, stripped, removedNumber }
}

function joinName({ label, stripped }: SplitTitle, title: string): string {
    return label && stripped ? `${label}: ${stripped}` : (label ?? stripped ?? title)
}

/** One line naming an item within its series: "Volume 3: The Long Road". */
export function itemName(c: Item, series: SeriesInfo | null | undefined): string {
    return joinName(splitItemTitle(c, series), c.title)
}

/** The parts of `readerTitle` after the series, or null when the item title stands alone. */
export function readerParts(c: Item, parent: SeriesInfo | null | undefined) {
    const s = splitItemTitle(c, parent)
    return parent && (s.label || s.stripped) ? { series: parent.title, ...s } : null
}

/** "Series · Volume 3: Subtitle"; the item title alone when that adds nothing. */
export function readerTitle(c: Item, parent: SeriesInfo | null | undefined): string {
    const p = readerParts(c, parent)
    return p ? `${p.series} · ${joinName(p, c.title)}` : c.title
}
