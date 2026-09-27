import { describe, expect, it } from 'vitest'
import type { BookStructure, SpineItem, TocEntry } from '@/utils/api/types'
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

function spine(items: Array<Partial<SpineItem> & { href: string }>): SpineItem[] {
    return items.map(item => ({
        title: item.href,
        linear: true,
        words: 100,
        ...item,
    }))
}

function toc(entries: Array<Partial<TocEntry> & { id: string }>): TocEntry[] {
    return entries.map(entry => ({
        title: entry.id,
        depth: 0,
        href: null,
        fragment: '',
        ...entry,
    }))
}

function fixture(
    spineItems: SpineItem[],
    tocEntries: TocEntry[],
    sources: Record<string, string>
): Fixture {
    const docs = new Map<string, Document>()
    const parser = new DOMParser()
    let stamp = 0
    for (const [href, html] of Object.entries(sources)) {
        const doc = parser.parseFromString(`<body>${html}</body>`, 'text/html')
        for (const el of Array.from(doc.body.querySelectorAll('*'))) {
            el.setAttribute('data-k', `k${stamp++}`)
        }
        docs.set(href, doc)
    }
    return { structure: { spine: spineItems, toc: tocEntries }, docs }
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

function flowLeaves({ structure, docs }: Fixture): string[] {
    return structure.spine
        .filter(item => item.linear)
        .flatMap(item => leaves(docs.get(item.href)!.body))
}

function pageLeaves({ docs }: Fixture, chapters: BookChapter[]): string[] {
    return chapters.flatMap(chapter =>
        chapter.slices.flatMap(slice => {
            const body = docs.get(slice.href)!.body.cloneNode(true) as HTMLElement
            pruneToRange(body, slice.start, slice.end)
            return leaves(body)
        })
    )
}

function words(text: string | null): string[] {
    return (text ?? '').split(/\s+/).filter(Boolean)
}

function flowText({ structure, docs }: Fixture): string {
    return structure.spine
        .filter(item => item.linear)
        .map(item => docs.get(item.href)!.body.textContent ?? '')
        .join('')
}

function pageText({ docs }: Fixture, chapters: BookChapter[]): string {
    return chapters
        .flatMap(chapter =>
            chapter.slices.map(slice => {
                const body = docs.get(slice.href)!.body.cloneNode(true) as HTMLElement
                pruneToRange(body, slice.start, slice.end)
                return body.textContent ?? ''
            })
        )
        .join('')
}

function pageWords(f: Fixture, chapters: BookChapter[]): string[] {
    return words(pageText(f, chapters))
}

/** Chapters must tile the linear flow: no gap, no overlap, no duplicate. */
function expectExactTiling(f: Fixture, chapters: BookChapter[]) {
    expect(pageText(f, chapters)).toBe(flowText(f))
    const rendered = pageLeaves(f, chapters)
    expect(rendered).toEqual(flowLeaves(f))
    expect(new Set(rendered).size).toBe(rendered.length)
    expect(chapters.every(chapter => chapter.slices.length > 0)).toBe(true)
}

const calibre = () =>
    fixture(
        spine([
            { href: 'part0001.html', title: 'Cover' },
            { href: 'part0002.html' },
            { href: 'part0003.html' },
            { href: 'part0004.html' },
        ]),
        toc([
            { id: 'c1', title: 'Chapter One', href: 'part0002.html' },
            { id: 'c2', title: 'Chapter Two', href: 'part0004.html' },
        ]),
        {
            'part0001.html': '<p>cover art</p>',
            'part0002.html': '<p>one begins</p>',
            'part0003.html': '<p>one continues</p>',
            'part0004.html': '<p>two begins</p>',
        }
    )

const oneFile = () =>
    fixture(
        spine([{ href: 'book.xhtml' }]),
        toc([
            { id: 'c1', title: 'One', href: 'book.xhtml', fragment: 'c1' },
            { id: 'c2', title: 'Two', href: 'book.xhtml', fragment: 'c2' },
            { id: 'c3', title: 'Three', href: 'book.xhtml', fragment: 'c3' },
        ]),
        {
            'book.xhtml': `<p>front matter</p>
                <h2 id="c1">One</h2> <p>alpha</p>
                <h2 id="c2">Two</h2> <p>beta</p>
                <h2 id="c3">Three</h2> <p>gamma</p>`,
        }
    )

describe('range construction', () => {
    it('merges Calibre splits and prepends front matter to chapter one', () => {
        const f = calibre()
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters.map(p => p.title)).toEqual(['Chapter One', 'Chapter Two'])
        expect(chapters[0]!.start).toEqual({ spineIndex: 0, path: [] })
        expect(chapters[0]!.slices.map(s => s.href)).toEqual([
            'part0001.html',
            'part0002.html',
            'part0003.html',
        ])
        expect(chapters[1]!.slices.map(s => s.href)).toEqual(['part0004.html'])
        expectExactTiling(f, chapters)
    })

    it('splits a one-file book into real chapters', () => {
        const f = oneFile()
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters).toHaveLength(3)
        expect(pageWords(f, [chapters[0]!])).toEqual(['front', 'matter', 'One', 'alpha'])
        expect(pageWords(f, [chapters[1]!])).toEqual(['Two', 'beta'])
        expect(pageWords(f, [chapters[2]!])).toEqual(['Three', 'gamma'])
        expectExactTiling(f, chapters)
    })

    it('absorbs an image-only interstitial into the preceding chapter, whole', () => {
        const f = fixture(
            spine([{ href: 'ch1.xhtml' }, { href: 'plate.xhtml' }, { href: 'ch2.xhtml' }]),
            toc([
                { id: 'c1', title: 'One', href: 'ch1.xhtml' },
                { id: 'c2', title: 'Two', href: 'ch2.xhtml' },
            ]),
            {
                'ch1.xhtml': '<p>one</p>',
                'plate.xhtml': '<img src="a.png" /><img src="b.png" />',
                'ch2.xhtml': '<p>two</p>',
            }
        )
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters).toHaveLength(2)
        expect(chapters[0]!.slices.map(s => s.href)).toEqual(['ch1.xhtml', 'plate.xhtml'])
        expect(pageLeaves(f, [chapters[0]!]).filter(leaf => leaf.includes(':')).length).toBe(3)
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
        const f = fixture(
            spine([{ href: 'doc.xhtml' }]),
            toc([
                { id: 'c1', title: 'One', href: 'doc.xhtml' },
                { id: 'c2', title: 'Two', href: 'doc.xhtml', fragment: 'inside' },
            ]),
            { 'doc.xhtml': markup }
        )
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters).toHaveLength(2)
        const second = f.docs.get('doc.xhtml')!.body.cloneNode(true) as HTMLElement
        pruneToRange(second, chapters[1]!.slices[0]!.start, chapters[1]!.slices[0]!.end)
        expect(second.querySelectorAll(child)).toHaveLength(2)
        expect(words(second.textContent)).toEqual(['a', 'b', 'after'])
        expectExactTiling(f, chapters)
    })

    it('drops a broken fragment without losing its content', () => {
        const f = fixture(
            spine([{ href: 'a.xhtml' }, { href: 'b.xhtml' }]),
            toc([
                { id: 'c1', title: 'One', href: 'a.xhtml' },
                { id: 'c2', title: 'Broken', href: 'b.xhtml', fragment: 'nope' },
            ]),
            { 'a.xhtml': '<p>one</p>', 'b.xhtml': '<p>two</p>' }
        )
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters).toHaveLength(1)
        expectExactTiling(f, chapters)
    })

    it('keeps linear="no" and off-spine documents out of the flow', () => {
        const f = fixture(
            spine([
                { href: 'ch1.xhtml' },
                { href: 'notes.xhtml', linear: false },
                { href: 'ch2.xhtml' },
            ]),
            toc([
                { id: 'c1', title: 'One', href: 'ch1.xhtml' },
                { id: 'n', title: 'Notes', href: 'notes.xhtml' },
                { id: 'ghost', title: 'Ghost', href: 'elsewhere.xhtml' },
                { id: 'c2', title: 'Two', href: 'ch2.xhtml' },
            ]),
            {
                'ch1.xhtml': '<p>one</p>',
                'notes.xhtml': '<p>note</p>',
                'ch2.xhtml': '<p>two</p>',
            }
        )
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters.map(p => p.title)).toEqual(['One', 'Two'])
        expect(chapters.flatMap(p => p.slices.map(s => s.href))).not.toContain('notes.xhtml')
        expectExactTiling(f, chapters)
    })

    it('treats a grouping label as its first linked child and dedupes aliases', () => {
        const f = fixture(
            spine([{ href: 'a.xhtml' }, { href: 'b.xhtml' }]),
            toc([
                { id: 'part1', title: 'Part One', depth: 0, href: null },
                { id: 'c1', title: 'One', depth: 1, href: 'a.xhtml' },
                { id: 'alias', title: 'One again', depth: 0, href: 'a.xhtml' },
                { id: 'c2', title: 'Two', depth: 0, href: 'b.xhtml' },
            ]),
            { 'a.xhtml': '<p>one</p>', 'b.xhtml': '<p>two</p>' }
        )
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters).toHaveLength(2)
        expect(chapters[0]!.title).toBe('Part One')
        expect(mapEntriesToChapters(f.structure, chapters, f.docs)).toEqual(
            new Map([
                ['c1', 0],
                ['alias', 0],
                ['c2', 1],
            ])
        )
        expectExactTiling(f, chapters)
    })

    it('resolves legacy name anchors', () => {
        const f = fixture(
            spine([{ href: 'a.xhtml' }]),
            toc([
                { id: 'c1', title: 'One', href: 'a.xhtml' },
                { id: 'c2', title: 'Two', href: 'a.xhtml', fragment: 'old' },
            ]),
            { 'a.xhtml': '<p>one</p> <a name="old"></a> <p>two</p>' }
        )
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters).toHaveLength(2)
        expectExactTiling(f, chapters)
    })

    it('has no chapters when no target resolves', () => {
        const f = fixture(
            spine([{ href: 'a.xhtml' }]),
            toc([{ id: 'c1', title: 'One', href: 'a.xhtml', fragment: 'missing' }]),
            { 'a.xhtml': '<p>one</p>' }
        )
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
        expect(
            hasUsableToc({
                spine: spine([{ href: 'a.xhtml', linear: false }]),
                toc: toc([{ id: 'x', href: 'a.xhtml' }]),
            })
        ).toBe(false)
        expect(hasUsableToc({ spine: spine([{ href: 'a.xhtml' }]), toc: toc([{ id: 'x' }]) })).toBe(
            false
        )
    })

    it('keeps top-level targets in authored order', () => {
        const targets = topLevelTargets(oneFile().structure)
        expect(targets.map(t => t.fragment)).toEqual(['c1', 'c2', 'c3'])
    })
})

describe('equivalent and extreme boundaries', () => {
    it('collapses a document-start target and its first-element target into one chapter', () => {
        const f = fixture(
            spine([{ href: 'a.xhtml' }, { href: 'b.xhtml' }]),
            toc([
                { id: 'doc', title: 'Doc', href: 'a.xhtml' },
                { id: 'start', title: 'Start', href: 'a.xhtml', fragment: 'start' },
                { id: 'next', title: 'Next', href: 'b.xhtml' },
            ]),
            {
                'a.xhtml': '<div><h1 id="start">Start</h1> <p>body</p></div>',
                'b.xhtml': '<p>second</p>',
            }
        )
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters).toHaveLength(2)
        expect(mapEntriesToChapters(f.structure, chapters, f.docs).get('start')).toBe(0)
        expect(pageWords(f, [chapters[0]!])).toEqual(['Start', 'body'])
        expectExactTiling(f, chapters)
    })

    it('collapses two targets that nudge onto the same table', () => {
        const f = fixture(
            spine([{ href: 'a.xhtml' }]),
            toc([
                { id: 'c1', title: 'One', href: 'a.xhtml' },
                { id: 'r1', title: 'Row one', href: 'a.xhtml', fragment: 'r1' },
                { id: 'r2', title: 'Row two', href: 'a.xhtml', fragment: 'r2' },
            ]),
            {
                'a.xhtml':
                    '<p>intro</p> <table><tbody><tr id="r1"><td>a</td></tr> <tr id="r2"><td>b</td></tr></tbody></table>',
            }
        )
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters).toHaveLength(2)
        const entries = mapEntriesToChapters(f.structure, chapters, f.docs)
        expect([entries.get('r1'), entries.get('r2')]).toEqual([1, 1])
        expectExactTiling(f, chapters)
    })

    it('handles targets on the very first and very last nodes', () => {
        const f = fixture(
            spine([{ href: 'a.xhtml' }]),
            toc([
                { id: 'first', title: 'First', href: 'a.xhtml', fragment: 'first' },
                { id: 'last', title: 'Last', href: 'a.xhtml', fragment: 'last' },
            ]),
            { 'a.xhtml': '<p id="first">one</p> <p>two</p> <p id="last">three</p>' }
        )
        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters).toHaveLength(2)
        expect(pageWords(f, [chapters[1]!])).toEqual(['three'])
        expectExactTiling(f, chapters)
    })

    it('keeps the attributes of an ancestor that both chapters share', () => {
        const f = fixture(
            spine([{ href: 'a.xhtml' }]),
            toc([
                { id: 'c1', title: 'One', href: 'a.xhtml' },
                { id: 'c2', title: 'Two', href: 'a.xhtml', fragment: 'c2' },
            ]),
            {
                'a.xhtml':
                    '<section class="chapter" lang="en"><p>one</p> <p id="c2">two</p></section>',
            }
        )
        const chapters = buildChapters(f.structure, f.docs)
        const second = f.docs.get('a.xhtml')!.body.cloneNode(true) as HTMLElement
        pruneToRange(second, chapters[1]!.slices[0]!.start, chapters[1]!.slices[0]!.end)
        const section = second.querySelector('section')!
        expect(section.getAttribute('class')).toBe('chapter')
        expect(section.getAttribute('lang')).toBe('en')
        expect(section.querySelectorAll('p')).toHaveLength(1)
        expectExactTiling(f, chapters)
    })
})

describe('targets on the body element', () => {
    it('gives a chapter whose target is its own body a chapter', () => {
        const f = fixture(
            spine([{ href: 'a.xhtml' }, { href: 'b.xhtml' }]),
            toc([
                { id: 'c1', title: 'One', href: 'a.xhtml' },
                { id: 'c2', title: 'Two', href: 'b.xhtml', fragment: 'chapter' },
            ]),
            { 'a.xhtml': '<p>one</p>', 'b.xhtml': '<p>two</p>' }
        )
        f.docs.get('b.xhtml')!.body.setAttribute('id', 'chapter')

        const chapters = buildChapters(f.structure, f.docs)
        expect(chapters.map(chapter => chapter.title)).toEqual(['One', 'Two'])
        expect(chapters[1]!.slices.map(slice => slice.href)).toEqual(['b.xhtml'])
        expectExactTiling(f, chapters)
    })
})
