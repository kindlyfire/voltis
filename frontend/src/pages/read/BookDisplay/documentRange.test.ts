import { describe, expect, it } from 'vitest'
import { textOffsetOfPoint, textPointAtOffset } from './documentRange'

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
                const offset = textOffsetOfPoint(root, node, index)
                expect(textPointAtOffset(root, offset)).toEqual({ node, index })
                checked++
            }
        }
        expect(checked).toBe(19)
    })

    it('counts a whitespace-only prefix as one collapsed space', () => {
        const root = parse('<p>\n    Hello</p>')
        const [node] = textNodes(root)
        expect(textOffsetOfPoint(root, node!, 5)).toBe(1)
    })

    it('keeps a whitespace run split across nodes as one space per node', () => {
        const root = parse('<p>a <b> b</b></p>')
        const [, second] = textNodes(root)
        expect(textOffsetOfPoint(root, second!, 1)).toBe(3)
        expect(textPointAtOffset(root, 3)).toEqual({ node: second, index: 1 })
        expect(textPointAtOffset(root, 2)).toEqual({ node: second, index: 1 })
    })

    it('moves past trailing whitespace into the next node', () => {
        const root = parse('<p>one  </p>\n<p>two</p>')
        const [, , two] = textNodes(root)
        expect(textPointAtOffset(root, 3)).toEqual({ node: two, index: 0 })
    })

    it('returns null outside the document', () => {
        const root = parse('<p>one</p>')
        expect(textPointAtOffset(root, -1)).toBeNull()
        expect(textPointAtOffset(root, 3)).toBeNull()
    })
})
