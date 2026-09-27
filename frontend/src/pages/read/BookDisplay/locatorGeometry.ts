import type { BookLocator } from '@/utils/api/types'
import { createTextIndex, type TextIndex } from './documentRange'
import {
    childNodesOf,
    childrenOf,
    findTarget,
    getAttr,
    parentOf,
    previousOf,
    tagOf,
    textWalkerFor,
} from './domSafe'

export const SCROLL_MARGIN = 8
const ANCHOR_SEARCH_LIMIT = 500

const BLOCK_TAGS = new Set([
    'p',
    'div',
    'section',
    'article',
    'blockquote',
    'pre',
    'figure',
    'img',
    'table',
    'tr',
    'td',
    'li',
    'h1',
    'h2',
    'h3',
    'h4',
    'h5',
    'h6',
])

/** Where the reader's view starts and ends, in reading order: a band of the
 * window in scroll mode, a screen of columns in paged mode. */
export interface VisibleFrame {
    /** Wholly before the view. */
    before(rect: DOMRect): boolean
    /** Wholly after the view. */
    after(rect: DOMRect): boolean
    /** A glyph that opens the view: on the line a restore aligns to. */
    accept(rect: DOMRect): boolean
    /** A glyph at least partly in view. */
    loose(rect: DOMRect): boolean
}

/** The 1px absorbs scroll rounding. */
export function scrollFrame(top: number, bottom: number): VisibleFrame {
    return {
        before: rect => rect.bottom <= top,
        after: rect => rect.top >= bottom,
        accept: rect => rect.top >= top + SCROLL_MARGIN - 1,
        loose: rect => rect.bottom > top,
    }
}

export function pagedFrame(left: number, right: number): VisibleFrame {
    return {
        before: rect => rect.right <= left,
        after: rect => rect.left >= right,
        accept: rect => rect.left >= left - 1,
        loose: rect => rect.right > left,
    }
}

export interface LocatableSlice {
    slice: { href: string }
    root: Element
    startOffset: number
}

/** Mounted slices never change their text, so each root's index is built once. */
const indexes = new WeakMap<Element, TextIndex>()

function textIndex(root: Element): TextIndex {
    let index = indexes.get(root)
    if (!index) indexes.set(root, (index = createTextIndex(root)))
    return index
}

function isEmpty(rect: DOMRect) {
    return rect.width === 0 && rect.height === 0
}

/** Out of flow, it can sit anywhere (an off-screen skip link or page marker). */
function outOfFlow(el: Element) {
    const position = getComputedStyle(el).position
    return position === 'absolute' || position === 'fixed'
}

/** The first child with a box not wholly before the view. Children flow in
 * reading order, so a binary search finds it without measuring every child
 * before the view; empty and out-of-flow boxes are stepped over. */
function firstChildInView(el: Element, frame: VisibleFrame): Element | null {
    const children = childrenOf(el)
    let lo = 0
    let hi = children.length
    let found: Element | null = null
    while (lo < hi) {
        const mid = (lo + hi) >> 1
        let i = mid
        let rect: DOMRect | null = null
        for (; i < hi; i++) {
            rect = children[i]!.getBoundingClientRect()
            if (!isEmpty(rect) && !outOfFlow(children[i]!)) break
        }
        if (i === hi) hi = mid
        else if (frame.before(rect!)) lo = i + 1
        else {
            found = children[i]!
            hi = mid
        }
    }
    return found
}

function firstVisibleBlock(root: Element, frame: VisibleFrame): Element | null {
    let best: Element | null = null
    for (let el = firstChildInView(root, frame); el; el = firstChildInView(el, frame)) {
        if (BLOCK_TAGS.has(tagOf(el))) best = el
    }
    return best
}

export function glyphRect(range: Range, node: Text, index: number): DOMRect | null {
    if (/\s/.test(node.data[index]!)) return null
    range.setStart(node, index)
    range.setEnd(node, index + 1)
    const rect = range.getBoundingClientRect()
    return isEmpty(rect) ? null : rect
}

/** The first usable character of `node` whose glyph `accept`s; whitespace and
 * hidden characters are stepped over to the next usable one. */
function firstGlyph(
    range: Range,
    node: Text,
    accept: (rect: DOMRect) => boolean
): { index: number; rect: DOMRect } | null {
    let lo = 0
    let hi = node.data.length
    let found: { index: number; rect: DOMRect } | null = null
    while (lo < hi) {
        const mid = (lo + hi) >> 1
        let index = mid
        let rect: DOMRect | null = null
        for (; index < hi; index++) if ((rect = glyphRect(range, node, index))) break
        if (!rect) hi = mid
        else if (accept(rect)) {
            found = { index, rect }
            hi = mid
        } else lo = index + 1
    }
    return found
}

interface Capture {
    slice: LocatableSlice
    el: Element
    offset: number
}

function* textNodesFrom(slices: LocatableSlice[], block: Element) {
    for (let i = 0; i < slices.length; i++) {
        const walker = textWalkerFor(slices[i]!.root)
        if (i === 0) walker.currentNode = block
        let node: Node | null
        while ((node = walker.nextNode())) yield { slice: slices[i]!, node: node as Text }
    }
}

function blockOf(el: Element, root: Element): Element {
    for (let node: Node | null = el; node && node !== root; node = parentOf(node)) {
        if (node.nodeType === Node.ELEMENT_NODE && BLOCK_TAGS.has(tagOf(node as Element))) {
            return node as Element
        }
    }
    return el
}

function blockStart(slice: LocatableSlice, el: Element): Capture {
    return { slice, el, offset: textIndex(slice.root).offsetOfNode(el) }
}

/** Glyph boxes sit inside line boxes, so after a restore the previous block's
 * end can still be inside the view: the search continues into later blocks and
 * slices rather than stopping at `block`. Vertical text falls back to the
 * start of its block. */
function scanGlyphs(
    slices: LocatableSlice[],
    block: Element,
    frame: VisibleFrame,
    accept: (rect: DOMRect) => boolean
): Capture | null {
    const range = new Range()
    for (const { slice, node } of textNodesFrom(slices, block)) {
        range.selectNodeContents(node)
        const rect = range.getBoundingClientRect()
        if (isEmpty(rect) || frame.before(rect)) continue
        if (frame.after(rect)) return null
        const parent = node.parentElement!
        if (getComputedStyle(parent).writingMode !== 'horizontal-tb') {
            return blockStart(slice, blockOf(parent, slice.root))
        }
        const glyph = firstGlyph(range, node, accept)
        if (!glyph) continue
        if (frame.after(glyph.rect)) return null
        return { slice, el: parent, offset: textIndex(slice.root).offsetOfPoint(node, glyph.index) }
    }
    return null
}

/** Captures against the line a restore aligns to, so a taller glyph on the
 * line above can't be taken for this one. */
function firstVisiblePoint(
    slices: LocatableSlice[],
    block: Element,
    frame: VisibleFrame
): Capture | null {
    return (
        scanGlyphs(slices, block, frame, frame.accept) ??
        scanGlyphs(slices, block, frame, frame.loose)
    )
}

/** The nearest authored id at or before `el` in document order, used to
 * recover a position when the document changed under a stored offset. */
function precedingAnchorId(el: Element, root: Element): string | undefined {
    let node: Node | null = el
    for (let steps = 0; node && node !== root && steps < ANCHOR_SEARCH_LIMIT; steps++) {
        if (node.nodeType === Node.ELEMENT_NODE) {
            const id = getAttr(node as Element, 'id') ?? getAttr(node as Element, 'name')
            if (id) return id
        }
        const previous = previousOf(node)
        if (!previous) {
            node = parentOf(node)
            continue
        }
        node = previous
        for (let last = childNodesOf(node).at(-1); last; last = childNodesOf(node).at(-1)) {
            node = last
        }
    }
    return undefined
}

/** The first visible character of `slices`, in reading order. */
export function captureIn(slices: LocatableSlice[], frame: VisibleFrame): BookLocator | null {
    for (let i = 0; i < slices.length; i++) {
        const slice = slices[i]!
        const block = firstVisibleBlock(slice.root, frame)
        if (!block) continue
        const found = firstVisiblePoint(slices.slice(i), block, frame) ?? blockStart(slice, block)
        return {
            version: 1,
            href: found.slice.slice.href,
            textOffset: found.slice.startOffset + found.offset,
            anchorId: precedingAnchorId(found.el, found.slice.root),
        }
    }
    return null
}

/** A single-character Range, never a collapsed one: at a line wrap Chrome
 * reports a collapsed Range at the end of the previous line. */
export function locateIn(slices: LocatableSlice[], locator: BookLocator): Element | Range | null {
    for (const slice of slices) {
        if (slice.slice.href !== locator.href) continue
        const relative = locator.textOffset - slice.startOffset
        if (relative < 0) continue
        const point = textIndex(slice.root).pointAtOffset(relative)
        if (!point) continue
        const range = new Range()
        return glyphRect(range, point.node, point.index)
            ? range
            : blockOf(point.node.parentElement!, slice.root)
    }
    if (!locator.anchorId) return null
    for (const slice of slices) {
        if (slice.slice.href !== locator.href) continue
        const el = findTarget(slice.root, locator.anchorId)
        if (el) return el
    }
    return null
}

/** Where `target` starts on screen. An empty element (a bare anchor) sits
 * where the next glyph does. */
export function firstRect(target: Element | Range): DOMRect | null {
    for (const rect of Array.from(target.getClientRects())) if (!isEmpty(rect)) return rect
    if (target instanceof Range) return null
    const walker = textWalkerFor(target.getRootNode())
    walker.currentNode = target
    const range = new Range()
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        const glyph = firstGlyph(range, node as Text, () => true)
        if (glyph) return glyph.rect
    }
    return null
}

/** The locator of a restore target itself, so a relayout returns to it rather
 * than to whatever opens the screen it landed on. */
export function locatorAt(slices: LocatableSlice[], target: Element | Range): BookLocator | null {
    const node = target instanceof Range ? target.startContainer : target
    const slice = slices.find(s => s.root.contains(node))
    if (!slice) return null
    const offset =
        target instanceof Range && node.nodeType === Node.TEXT_NODE
            ? textIndex(slice.root).offsetOfPoint(node as Text, target.startOffset)
            : textIndex(slice.root).offsetOfNode(node)
    const el = node.nodeType === Node.ELEMENT_NODE ? (node as Element) : node.parentElement!
    return {
        version: 1,
        href: slice.slice.href,
        textOffset: slice.startOffset + offset,
        anchorId: precedingAnchorId(el, slice.root),
    }
}

export function locateAnchorIn(
    slices: LocatableSlice[],
    anchor: { href: string; fragment: string }
): Element | null {
    for (const slice of slices) {
        if (slice.slice.href !== anchor.href) continue
        const el = findTarget(slice.root, anchor.fragment)
        if (el) return el
    }
    return null
}
