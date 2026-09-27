import { describe, expect, it } from 'vitest'
import type { BookStructure } from '@/utils/api/types'
import {
    buildChapters,
    hasUsableToc,
    mapEntriesToChapters,
    chaptersContainingOffset,
    chapterIndexForPosition,
    resolveFlowPosition,
    topLevelTargets,
    type BookChapter,
} from './buildChapters'
import { pruneToRange } from './documentRange'

interface Fixture {
    structure: BookStructure
    docs: Map<string, Document>
}

/** The spine is the documents in order (a trailing `!` marks linear="no"); TOC
 * rows are `[id (also the title), href, fragment?, depth?]`. Every element gets
 * a unique `data-k`, so leaves can be told apart. */
function fixture(
    sources: Record<string, string>,
    toc: Array<[string, string | null, string?, number?]>
): Fixture {
    const docs = new Map<string, Document>()
    let stamp = 0
    for (const [key, html] of Object.entries(sources)) {
        const doc = new DOMParser().parseFromString(`<body>${html}</body>`, 'text/html')
        for (const el of Array.from(doc.body.querySelectorAll('*'))) {
            el.setAttribute('data-k', `k${stamp++}`)
        }
        docs.set(key.replace('!', ''), doc)
    }
    const spine = Object.keys(sources).map(key => ({
        href: key.replace('!', ''),
        title: key,
        linear: !key.endsWith('!'),
        words: 100,
    }))
    const entries = toc.map(([id, href, fragment = '', depth = 0]) => ({
        id,
        title: id,
        depth,
        href,
        fragment,
    }))
    return { structure: { spine, toc: entries }, docs }
}

/** Every leaf the flow renders — textless ones included — so a dropped or
 * duplicated image is caught as readily as missing text. */
function leaves(root: Element): string[] {
    const out: string[] = []
    const walk = (node: Node) => {
        for (const child of Array.from(node.childNodes)) {
            if (child.nodeType === Node.TEXT_NODE) {
                const text = (child as Text).data.trim()
                if (text) out.push(`${(node as Element).getAttribute?.('data-k') ?? '-'}#${text}`)
            } else if (child.nodeType === Node.ELEMENT_NODE) {
                const el = child as Element
                if (el.firstElementChild) walk(el)
                else out.push(`${el.getAttribute('data-k')}:${el.textContent?.trim() ?? ''}`)
            }
        }
    }
    walk(root)
    return out
}

/** Each slice's document body, pruned to the slice. */
function sliceBodies({ docs }: Fixture, chapters: BookChapter[]): HTMLElement[] {
    return chapters.flatMap(chapter =>
        chapter.slices.map(slice => {
            const body = docs.get(slice.href)!.body.cloneNode(true) as HTMLElement
            pruneToRange(body, slice.start, slice.end)
            return body
        })
    )
}

const textOf = (bodies: HTMLElement[]) => bodies.map(body => body.textContent ?? '').join('')
const wordsOf = (f: Fixture, chapter: BookChapter) =>
    textOf(sliceBodies(f, [chapter]))
        .split(/\s+/)
        .filter(Boolean)

/** Chapters must tile the linear flow: no gap, no overlap, no duplicate. */
function expectExactTiling(f: Fixture, chapters: BookChapter[]) {
    const flow = f.structure.spine
        .filter(item => item.linear)
        .map(item => f.docs.get(item.href)!.body)
    const pages = sliceBodies(f, chapters)
    expect(textOf(pages)).toBe(textOf(flow))
    const rendered = pages.flatMap(leaves)
    expect(rendered).toEqual(flow.flatMap(leaves))
    expect(new Set(rendered).size).toBe(rendered.length)
    expect(chapters.every(chapter => chapter.slices.length > 0)).toBe(true)
}

const outline = (chapters: BookChapter[]) =>
    chapters.map(chapter => [chapter.title, chapter.slices.map(slice => slice.href)])

const calibre = () =>
    fixture(
        {
            'part0001.html': '<p>cover art</p>',
            'part0002.html': '<p>one begins</p>',
            'part0003.html': '<p>one continues</p>',
            'part0004.html': '<p>two begins</p>',
        },
        [
            ['Chapter One', 'part0002.html'],
            ['Chapter Two', 'part0004.html'],
        ]
    )

const oneFile = () =>
    fixture(
        {
            'book.xhtml': `<p>front matter</p>
                <h2 id="c1">One</h2> <p>alpha</p>
                <h2 id="c2">Two</h2> <p>beta</p>
                <h2 id="c3">Three</h2> <p>gamma</p>`,
        },
        [
            ['One', 'book.xhtml', 'c1'],
            ['Two', 'book.xhtml', 'c2'],
            ['Three', 'book.xhtml', 'c3'],
        ]
    )

describe('range construction', () => {
    it('merges Calibre splits and prepends front matter to chapter one', () => {
        const f = calibre()
        const chapters = buildChapters(f.structure, f.docs)
        expect(outline(chapters)).toEqual([
            ['Chapter One', ['part0001.html', 'part0002.html', 'part0003.html']],
            ['Chapter Two', ['part0004.html']],
        ])
        expect(chapters[0]!.start).toEqual({ spineIndex: 0, path: [] })
        expectExactTiling(f, chapters)
    })

    it('splits a one-file book into real chapters', () => {
        const f = oneFile()
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters.map(chapter => wordsOf(f, chapter))).toEqual([
            ['front', 'matter', 'One', 'alpha'],
            ['Two', 'beta'],
            ['Three', 'gamma'],
        ])
        expectExactTiling(f, chapters)
    })

    it('absorbs an image-only interstitial into the preceding chapter, whole', () => {
        const f = fixture(
            {
                'ch1.xhtml': '<p>one</p>',
                'plate.xhtml': '<img src="a.png" /><img src="b.png" />',
                'ch2.xhtml': '<p>two</p>',
            },
            [
                ['One', 'ch1.xhtml'],
                ['Two', 'ch2.xhtml'],
            ]
        )
        const chapters = buildChapters(f.structure, f.docs)
        expect(outline(chapters)).toEqual([
            ['One', ['ch1.xhtml', 'plate.xhtml']],
            ['Two', ['ch2.xhtml']],
        ])
        const elements = sliceBodies(f, [chapters[0]!]).flatMap(leaves)
        expect(elements.filter(leaf => leaf.includes(':'))).toHaveLength(3)
        expectExactTiling(f, chapters)
    })

    it.each([
        [
            'table',
            `<p>before</p> <table><tbody><tr><td>a</td></tr> <tr><td id="inside">b</td></tr></tbody></table> <p>after</p>`,
            'tr',
        ],
        ['list', `<p>before</p> <ul><li>a</li> <li id="inside">b</li></ul> <p>after</p>`, 'li'],
    ])('nudges a boundary inside a %s out to the whole of it', (_kind, markup, child) => {
        const f = fixture({ 'doc.xhtml': markup }, [
            ['One', 'doc.xhtml'],
            ['Two', 'doc.xhtml', 'inside'],
        ])
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters).toHaveLength(2)
        const [second] = sliceBodies(f, [chapters[1]!])
        expect(second!.querySelectorAll(child)).toHaveLength(2)
        expect(wordsOf(f, chapters[1]!)).toEqual(['a', 'b', 'after'])
        expectExactTiling(f, chapters)
    })

    const twoDocs = { 'a.xhtml': '<p>one</p>', 'b.xhtml': '<p>two</p>' }
    it.each([
        [
            'drops a broken fragment without losing its content',
            () =>
                fixture(twoDocs, [
                    ['One', 'a.xhtml'],
                    ['Broken', 'b.xhtml', 'nope'],
                ]),
            [['One', ['a.xhtml', 'b.xhtml']]],
        ],
        [
            'resolves legacy name anchors',
            () =>
                fixture({ 'a.xhtml': '<p>one</p> <a name="old"></a> <p>two</p>' }, [
                    ['One', 'a.xhtml'],
                    ['Two', 'a.xhtml', 'old'],
                ]),
            [
                ['One', ['a.xhtml']],
                ['Two', ['a.xhtml']],
            ],
        ],
        [
            'gives a target naming its own body a chapter',
            () => {
                const f = fixture(twoDocs, [
                    ['One', 'a.xhtml'],
                    ['Two', 'b.xhtml', 'chapter'],
                ])
                f.docs.get('b.xhtml')!.body.id = 'chapter'
                return f
            },
            [
                ['One', ['a.xhtml']],
                ['Two', ['b.xhtml']],
            ],
        ],
        [
            'keeps linear="no" and off-spine documents out of the flow',
            () =>
                fixture(
                    {
                        'ch1.xhtml': '<p>one</p>',
                        'notes.xhtml!': '<p>note</p>',
                        'ch2.xhtml': '<p>two</p>',
                    },
                    [
                        ['One', 'ch1.xhtml'],
                        ['Notes', 'notes.xhtml'],
                        ['Ghost', 'elsewhere.xhtml'],
                        ['Two', 'ch2.xhtml'],
                    ]
                ),
            [
                ['One', ['ch1.xhtml']],
                ['Two', ['ch2.xhtml']],
            ],
        ],
    ])('%s', (_name, make, expected) => {
        const f = make()
        const chapters = buildChapters(f.structure, f.docs)
        expect(outline(chapters)).toEqual(expected)
        expectExactTiling(f, chapters)
    })

    it('treats a grouping label as its first linked child and dedupes aliases', () => {
        const f = fixture(twoDocs, [
            ['Part One', null],
            ['One', 'a.xhtml', '', 1],
            ['One again', 'a.xhtml'],
            ['Two', 'b.xhtml'],
        ])
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters.map(chapter => chapter.title)).toEqual(['Part One', 'Two'])
        expect(mapEntriesToChapters(f.structure, chapters, f.docs)).toEqual(
            new Map([
                ['One', 0],
                ['One again', 0],
                ['Two', 1],
            ])
        )
        expectExactTiling(f, chapters)
    })

    it('has no chapters when no target resolves', () => {
        const f = fixture({ 'a.xhtml': '<p>one</p>' }, [['One', 'a.xhtml', 'missing']])
        expect(buildChapters(f.structure, f.docs)).toEqual([])
        expect(hasUsableToc(f.structure)).toBe(true)
    })
})

describe('lookups', () => {
    it('finds the chapter holding a position and an offset', () => {
        const f = oneFile()
        const chapters = buildChapters(f.structure, f.docs)
        const second = resolveFlowPosition(f.structure, f.docs, 'book.xhtml', 'c2')!
        expect(chapterIndexForPosition(chapters, second)).toBe(1)

        const doc = f.docs.get('book.xhtml')!
        expect(chaptersContainingOffset(chapters, 'book.xhtml', 0, doc)).toEqual([0])
        const total = doc.body.textContent!.replace(/\s+/g, ' ').length
        expect(chaptersContainingOffset(chapters, 'book.xhtml', total, doc)).toEqual([2])
    })

    it('reports whether the TOC is usable at all', () => {
        expect(hasUsableToc(calibre().structure)).toBe(true)
        expect(hasUsableToc(fixture({ 'a.xhtml!': '' }, [['x', 'a.xhtml']]).structure)).toBe(false)
        expect(hasUsableToc(fixture({ 'a.xhtml': '' }, [['x', null]]).structure)).toBe(false)
    })

    it('keeps top-level targets in authored order', () => {
        const targets = topLevelTargets(oneFile().structure)
        expect(targets.map(t => t.fragment)).toEqual(['c1', 'c2', 'c3'])
    })
})

describe('equivalent and extreme boundaries', () => {
    it('collapses a document-start target and its first-element target into one chapter', () => {
        const f = fixture(
            {
                'a.xhtml': '<div><h1 id="start">Start</h1> <p>body</p></div>',
                'b.xhtml': '<p>second</p>',
            },
            [
                ['doc', 'a.xhtml'],
                ['start', 'a.xhtml', 'start'],
                ['next', 'b.xhtml'],
            ]
        )
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters).toHaveLength(2)
        expect(mapEntriesToChapters(f.structure, chapters, f.docs).get('start')).toBe(0)
        expect(wordsOf(f, chapters[0]!)).toEqual(['Start', 'body'])
        expectExactTiling(f, chapters)
    })

    it('collapses two targets that nudge onto the same table', () => {
        const f = fixture(
            {
                'a.xhtml':
                    '<p>intro</p> <table><tbody><tr id="r1"><td>a</td></tr> <tr id="r2"><td>b</td></tr></tbody></table>',
            },
            [
                ['c1', 'a.xhtml'],
                ['r1', 'a.xhtml', 'r1'],
                ['r2', 'a.xhtml', 'r2'],
            ]
        )
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters).toHaveLength(2)
        const entries = mapEntriesToChapters(f.structure, chapters, f.docs)
        expect([entries.get('r1'), entries.get('r2')]).toEqual([1, 1])
        expectExactTiling(f, chapters)
    })

    it('handles targets on the very first and very last nodes', () => {
        const f = fixture(
            { 'a.xhtml': '<p id="first">one</p> <p>two</p> <p id="last">three</p>' },
            [
                ['First', 'a.xhtml', 'first'],
                ['Last', 'a.xhtml', 'last'],
            ]
        )
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters).toHaveLength(2)
        expect(wordsOf(f, chapters[1]!)).toEqual(['three'])
        expectExactTiling(f, chapters)
    })

    it('keeps the attributes of an ancestor that both chapters share', () => {
        const f = fixture(
            {
                'a.xhtml':
                    '<section class="chapter" lang="en"><p>one</p> <p id="c2">two</p></section>',
            },
            [
                ['One', 'a.xhtml'],
                ['Two', 'a.xhtml', 'c2'],
            ]
        )
        const chapters = buildChapters(f.structure, f.docs)
        const section = sliceBodies(f, [chapters[1]!])[0]!.querySelector('section')!
        expect(section.getAttribute('class')).toBe('chapter')
        expect(section.getAttribute('lang')).toBe('en')
        expect(section.querySelectorAll('p')).toHaveLength(1)
        expectExactTiling(f, chapters)
    })
})
