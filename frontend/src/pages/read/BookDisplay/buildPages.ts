import type { BookStructure, TocEntry } from '@/utils/api/types'
import { canonicalStart, comparePaths, textOffsetAtPath, type NodePath } from './documentRange'
import { docBody, findTarget, parentOf, tagOf } from './domSafe'

export interface FlowPosition {
    spineIndex: number
    /** Empty means the start of the document. */
    path: NodePath
}

export interface PageSlice {
    href: string
    spineIndex: number
    start: NodePath | null
    end: NodePath | null
}

export interface TocTarget {
    title: string
    href: string
    fragment: string
}

export interface BookPage {
    index: number
    title: string
    /** The authored target, which page 1 may sit below. */
    target: { href: string; fragment: string }
    start: FlowPosition
    end: FlowPosition | null
    slices: PageSlice[]
}

const NUDGE_TAGS = new Set(['table', 'ul', 'ol', 'dl', 'figure'])

export function spineIndexOf(structure: BookStructure, href: string): number {
    return structure.spine.findIndex(item => item.href === href)
}

/** The cheap content-page check: is any TOC entry pointing at a linear spine
 * document? No fragments are resolved. */
export function hasUsableToc(structure: BookStructure): boolean {
    return structure.toc.some(
        entry =>
            entry.href != null &&
            structure.spine.some(item => item.href === entry.href && item.linear)
    )
}

/** Top-level entries, with href-less grouping labels made transparent by
 * borrowing their first linked descendant. */
export function topLevelTargets(structure: BookStructure): TocTarget[] {
    const toc = structure.toc
    const targets: TocTarget[] = []
    for (let i = 0; i < toc.length; i++) {
        const entry = toc[i]!
        if (entry.depth !== 0) continue
        let source: TocEntry | null = entry.href != null ? entry : null
        for (let j = i + 1; !source && j < toc.length && toc[j]!.depth > 0; j++) {
            if (toc[j]!.href != null) source = toc[j]!
        }
        if (!source?.href) continue
        targets.push({
            title: entry.title || source.title,
            href: source.href,
            fragment: source.fragment,
        })
    }
    return targets
}

export function boundaryDocumentHrefs(structure: BookStructure): string[] {
    const hrefs = new Set<string>()
    for (const target of topLevelTargets(structure)) {
        if (!target.fragment) continue
        const index = spineIndexOf(structure, target.href)
        if (index !== -1 && structure.spine[index]!.linear) hrefs.add(target.href)
    }
    return [...hrefs]
}

export function resolveTargetElement(doc: Document, fragment: string): Element | null {
    const body = docBody(doc)
    return body ? findTarget(body, fragment) : null
}

/** A cut may never land inside a table, list or figure, so a boundary inside
 * one moves out to the start of the outermost such ancestor. */
export function nudgeBoundary(el: Element, root: Element): Element {
    let outermost: Element = el
    let current = parentOf(el)
    while (current && current !== root && current.nodeType === Node.ELEMENT_NODE) {
        if (NUDGE_TAGS.has(tagOf(current as Element))) outermost = current as Element
        current = parentOf(current)
    }
    return outermost
}

export function resolveFlowPosition(
    structure: BookStructure,
    docs: Map<string, Document>,
    href: string,
    fragment: string
): FlowPosition | null {
    const spineIndex = spineIndexOf(structure, href)
    if (spineIndex === -1 || !structure.spine[spineIndex]!.linear) return null
    if (!fragment) return { spineIndex, path: [] }

    const doc = docs.get(href)
    const body = doc ? docBody(doc) : null
    if (!body) return null
    const el = resolveTargetElement(doc!, fragment)
    if (!el) return null
    return { spineIndex, path: canonicalStart(body, nudgeBoundary(el, body)) }
}

export function comparePositions(a: FlowPosition, b: FlowPosition): number {
    return a.spineIndex - b.spineIndex || comparePaths(a.path, b.path)
}

function sliceRange(
    structure: BookStructure,
    start: FlowPosition,
    end: FlowPosition | null
): PageSlice[] {
    const slices: PageSlice[] = []
    const last = end ? end.spineIndex : structure.spine.length - 1
    for (let i = start.spineIndex; i <= last; i++) {
        const item = structure.spine[i]!
        if (!item.linear) continue
        const isEndDoc = end != null && i === end.spineIndex
        if (isEndDoc && end!.path.length === 0) continue
        slices.push({
            href: item.href,
            spineIndex: i,
            start: i === start.spineIndex && start.path.length ? start.path : null,
            end: isEndDoc ? end!.path : null,
        })
    }
    return slices
}

export function buildPages(structure: BookStructure, docs: Map<string, Document>): BookPage[] {
    const firstLinear = structure.spine.findIndex(item => item.linear)
    if (firstLinear === -1) return []

    const resolved: Array<{ position: FlowPosition; target: TocTarget }> = []
    for (const target of topLevelTargets(structure)) {
        const position = resolveFlowPosition(structure, docs, target.href, target.fragment)
        if (position) resolved.push({ position, target })
    }
    if (!resolved.length) return []

    resolved.sort((a, b) => comparePositions(a.position, b.position))

    const groups: Array<{ position: FlowPosition; targets: TocTarget[] }> = []
    for (const item of resolved) {
        const previous = groups[groups.length - 1]
        if (previous && comparePositions(previous.position, item.position) === 0) {
            previous.targets.push(item.target)
        } else {
            groups.push({ position: item.position, targets: [item.target] })
        }
    }

    const flowStart: FlowPosition = { spineIndex: firstLinear, path: [] }
    return groups.map((group, index) => {
        const start = index === 0 ? flowStart : group.position
        const end = groups[index + 1]?.position ?? null
        const primary = group.targets[0]!
        return {
            index,
            title: primary.title,
            target: { href: primary.href, fragment: primary.fragment },
            start,
            end,
            slices: sliceRange(structure, start, end),
        }
    })
}

export function pageIndexForPosition(pages: BookPage[], position: FlowPosition): number {
    for (let i = pages.length - 1; i >= 0; i--) {
        if (comparePositions(pages[i]!.start, position) <= 0) return i
    }
    return pages.length ? 0 : -1
}

export function sliceTextRange(doc: Document, slice: PageSlice) {
    const body = docBody(doc)!
    return {
        start: textOffsetAtPath(body, slice.start),
        end: slice.end ? textOffsetAtPath(body, slice.end) : Number.POSITIVE_INFINITY,
    }
}

/** Every page whose slice of `href` covers `textOffset`. A textless slice has
 * an empty interval, so more than one page can qualify. */
export function pagesContainingOffset(
    pages: BookPage[],
    href: string,
    textOffset: number,
    doc: Document | null
): number[] {
    const candidates = pages.filter(page => page.slices.some(slice => slice.href === href))
    if (!candidates.length) return []
    if (candidates.length === 1 || !doc) return [candidates[0]!.index]

    const covering = candidates.filter(page =>
        page.slices.some(slice => {
            if (slice.href !== href) return false
            const range = sliceTextRange(doc, slice)
            if (range.start === range.end) return textOffset === range.start
            return textOffset >= range.start && textOffset < range.end
        })
    )
    return (covering.length ? covering : [candidates[candidates.length - 1]!]).map(
        page => page.index
    )
}

/** Which page each TOC entry belongs to, exact where the entry's document is
 * already prepared and document-level otherwise. */
export function mapEntriesToPages(
    structure: BookStructure,
    pages: BookPage[],
    docs: Map<string, Document>
): Map<string, number> {
    const map = new Map<string, number>()
    for (const entry of structure.toc) {
        if (entry.href == null) continue
        const position =
            resolveFlowPosition(structure, docs, entry.href, entry.fragment) ??
            resolveFlowPosition(structure, docs, entry.href, '')
        if (!position) continue
        const index = pageIndexForPosition(pages, position)
        if (index !== -1) map.set(entry.id, index)
    }
    return map
}
