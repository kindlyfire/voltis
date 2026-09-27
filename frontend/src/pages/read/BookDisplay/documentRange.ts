import { childNodesOf, parentOf, previousOf, textWalkerFor } from './domSafe'

/** Child-index path from a root node, using `childNodes` so text nodes count
 * too. `[]` is the root itself; `null` means unbounded. */
export type NodePath = number[]

export function comparePaths(a: NodePath, b: NodePath): number {
    const n = Math.min(a.length, b.length)
    for (let i = 0; i < n; i++) {
        if (a[i]! !== b[i]!) return a[i]! - b[i]!
    }
    return a.length - b.length
}

function isStrictPrefix(a: NodePath, b: NodePath): boolean {
    if (a.length >= b.length) return false
    for (let i = 0; i < a.length; i++) {
        if (a[i] !== b[i]) return false
    }
    return true
}

export function pathOfNode(root: Node, node: Node): NodePath | null {
    const path: NodePath = []
    let current: Node | null = node
    while (current && current !== root) {
        const parent: Node | null = parentOf(current)
        if (!parent) return null
        const index = childNodesOf(parent).indexOf(current)
        if (index === -1) return null
        path.unshift(index)
        current = parent
    }
    return current === root ? path : null
}

export function nodeAtPath(root: Node, path: NodePath): Node | null {
    let node: Node = root
    for (const index of path) {
        const next = childNodesOf(node)[index]
        if (!next) return null
        node = next
    }
    return node
}

function classify(p: NodePath, start: NodePath | null, end: NodePath | null) {
    let partial = false
    if (start) {
        if (isStrictPrefix(p, start)) partial = true
        else if (comparePaths(p, start) < 0) return 'out'
    }
    if (end) {
        if (isStrictPrefix(p, end)) partial = true
        else if (comparePaths(p, end) >= 0) return 'out'
    }
    return partial ? 'partial' : 'in'
}

/** Whether the node at `p` intersects `[start, end)`. */
export function pathInRange(p: NodePath, start: NodePath | null, end: NodePath | null): boolean {
    return classify(p, start, end) !== 'out'
}

/** Drops every subtree wholly outside `[start, end)`, keeping the ancestors
 * that intersect it. Boundaries always sit before an element, so no text node
 * is ever split. Mutates `root`, which must be a clone. */
export function pruneToRange(root: Node, start: NodePath | null, end: NodePath | null) {
    if (!start && !end) return
    const walk = (node: Node, path: NodePath) => {
        const children = childNodesOf(node)
        for (let i = 0; i < children.length; i++) {
            const child = children[i]!
            const p = [...path, i]
            const rel = classify(p, start, end)
            if (rel === 'out') (child as ChildNode).remove()
            else if (rel === 'partial') walk(child, p)
        }
    }
    walk(root, [])
}

export function hasContentBefore(node: Node): boolean {
    for (let sibling = previousOf(node); sibling; sibling = previousOf(sibling)) {
        if (sibling.nodeType === Node.TEXT_NODE) {
            if ((sibling as Text).data.trim()) return true
        } else if (sibling.nodeType === Node.ELEMENT_NODE) {
            return true
        }
    }
    return false
}

/** The outermost position equivalent to a cut before `node`: two boundaries
 * with nothing between them must become one page, not an empty one. */
export function canonicalStart(root: Node, node: Node): NodePath {
    let current: Node = node
    for (;;) {
        if (hasContentBefore(current)) break
        const parent = parentOf(current)
        if (!parent) break
        if (parent === root) return []
        current = parent
    }
    return pathOfNode(root, current) ?? []
}

function contribution(data: string): number {
    const collapsed = data.replace(/\s+/g, ' ')
    return collapsed.trim() === '' ? 0 : collapsed.length
}

export function normalizedTextLength(root: Node | null): number {
    if (!root) return 0
    const walker = textWalkerFor(root)
    let total = 0
    let node: Node | null
    while ((node = walker.nextNode())) total += contribution((node as Text).data)
    return total
}

export function textOffsetOfNode(root: Node, node: Node): number {
    if (node === root) return 0
    const walker = textWalkerFor(root)
    let total = 0
    let text: Node | null
    while ((text = walker.nextNode())) {
        const position = node.compareDocumentPosition(text)
        if (!(position & Node.DOCUMENT_POSITION_PRECEDING)) break
        total += contribution((text as Text).data)
    }
    return total
}

export function textOffsetAtPath(root: Node, path: NodePath | null): number {
    if (!path || path.length === 0) return 0
    const node = nodeAtPath(root, path)
    return node ? textOffsetOfNode(root, node) : 0
}

export interface TextPoint {
    node: Text
    index: number
}

function rawIndex(data: string, collapsed: number): number {
    let count = 0
    for (let i = 0; i < data.length; i++) {
        if (count === collapsed) return i
        if (!/\s/.test(data[i]!) || !/\s/.test(data[i + 1] ?? '')) count++
    }
    return data.length
}

/** The text offsets of a tree whose text no longer changes, such as a mounted
 * slice: built once, so lookups are O(log n) rather than a walk from the
 * start. */
export interface TextIndex {
    offsetOfNode(node: Node): number
    /** Collapses the prefix itself: `contribution` counts a whitespace-only
     * prefix as 0, which breaks the round trip for `"\n  Hello"`. */
    offsetOfPoint(node: Text, index: number): number
    /** The first non-whitespace character at or after `offset`: block-start
     * offsets often land on indentation, which has no rect of its own. Null
     * when the offset falls outside the document, so a stale locator falls
     * back to its anchor rather than silently landing on the last paragraph. */
    pointAtOffset(offset: number): TextPoint | null
}

export function createTextIndex(root: Node): TextIndex {
    const texts: Text[] = []
    const at = new Map<Text, number>()
    /** `starts[i]` is the offset where `texts[i]` begins; one extra entry holds
     * the total. */
    const starts = [0]
    const walker = textWalkerFor(root)
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        at.set(node as Text, texts.length)
        texts.push(node as Text)
        starts.push(starts.at(-1)! + contribution((node as Text).data))
    }

    /** The first `i` in `[0, texts.length]` for which `test(i)` holds, given
     * that it holds for every later `i` too. */
    const search = (test: (i: number) => boolean) => {
        let lo = 0
        let hi = texts.length
        while (lo < hi) {
            const mid = (lo + hi) >> 1
            if (test(mid)) hi = mid
            else lo = mid + 1
        }
        return lo
    }

    const offsetOfNode = (node: Node) => {
        if (node === root) return 0
        const i =
            at.get(node as Text) ??
            search(
                i => !(node.compareDocumentPosition(texts[i]!) & Node.DOCUMENT_POSITION_PRECEDING)
            )
        return starts[i]!
    }

    return {
        offsetOfNode,
        offsetOfPoint: (node, index) =>
            offsetOfNode(node) + node.data.slice(0, index).replace(/\s+/g, ' ').length,
        pointAtOffset(offset) {
            if (offset < 0) return null
            const first = search(i => starts[i + 1]! > offset)
            for (let i = first; i < texts.length; i++) {
                const data = texts[i]!.data
                const from = i === first ? rawIndex(data, offset - starts[i]!) : 0
                const index = data.slice(from).search(/\S/)
                if (index !== -1) return { node: texts[i]!, index: from + index }
            }
            return null
        },
    }
}
