import { describe, expect, it } from 'vitest'
import { createTextIndex, textOffsetOfNode } from './documentRange'

function parse(html: string): HTMLElement {
    const root = document.createElement('div')
    root.innerHTML = html
    return root
}

function textNodes(root: Node): Text[] {
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
    const out: Text[] = []
    for (let node = walker.nextNode(); node; node = walker.nextNode()) out.push(node as Text)
    return out
}

describe('text points', () => {
    it('round-trips every non-whitespace character', () => {
        const root = parse(
            '<p>\n  Hello  world <b>big</b>\n</p>\n  <p><i>  x </i>y\t\tz</p><p> </p><p>end</p>'
        )
        let checked = 0
        for (const node of textNodes(root)) {
            for (let index = 0; index < node.data.length; index++) {
                if (/\s/.test(node.data[index]!)) continue
                const offset = createTextIndex(root).offsetOfPoint(node, index)
                expect(createTextIndex(root).pointAtOffset(offset)).toEqual({ node, index })
                checked++
            }
        }
        expect(checked).toBe(19)
    })

    it('counts a whitespace-only prefix as one collapsed space', () => {
        const root = parse('<p>\n    Hello</p>')
        const [node] = textNodes(root)
        expect(createTextIndex(root).offsetOfPoint(node!, 5)).toBe(1)
    })

    it('keeps a whitespace run split across nodes as one space per node', () => {
        const root = parse('<p>a <b> b</b></p>')
        const [, second] = textNodes(root)
        expect(createTextIndex(root).offsetOfPoint(second!, 1)).toBe(3)
        expect(createTextIndex(root).pointAtOffset(3)).toEqual({ node: second, index: 1 })
        expect(createTextIndex(root).pointAtOffset(2)).toEqual({ node: second, index: 1 })
    })

    it('moves past trailing whitespace into the next node', () => {
        const root = parse('<p>one  </p>\n<p>two</p>')
        const [, , two] = textNodes(root)
        expect(createTextIndex(root).pointAtOffset(3)).toEqual({ node: two, index: 0 })
    })

    it('returns null outside the document', () => {
        const root = parse('<p>one</p>')
        expect(createTextIndex(root).pointAtOffset(-1)).toBeNull()
        expect(createTextIndex(root).pointAtOffset(3)).toBeNull()
    })
})

describe('text index', () => {
    const html =
        '<p>\n  Hello  world <b>big</b><!-- c -->\n</p>\n  <p><i>  x </i>y\t\tz</p><p> </p><img><p><span></span>end</p><p></p>'

    it('matches the walk from the start for every node', () => {
        const root = parse(html)
        const index = createTextIndex(root)
        const all = [root, ...Array.from(root.querySelectorAll('*'))]
        const walker = document.createTreeWalker(root, NodeFilter.SHOW_ALL)
        for (let node = walker.nextNode(); node; node = walker.nextNode()) all.push(node as Element)
        for (const node of all) expect(index.offsetOfNode(node)).toBe(textOffsetOfNode(root, node))
    })

    it('finds the point at every offset, and none past the end', () => {
        const root = parse(html)
        const index = createTextIndex(root)
        const total = index.offsetOfNode(root.lastChild!)
        for (let offset = 0; offset < total; offset++) {
            const point = index.pointAtOffset(offset)!
            expect(index.offsetOfPoint(point.node, point.index)).toBeGreaterThanOrEqual(offset)
        }
        expect(index.pointAtOffset(total)).toBeNull()
    })
})
