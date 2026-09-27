import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { BookChapter, ChapterSlice } from './buildChapters'
import { createChapterPrefetch, sliceImages } from './chapterPrefetch'
import type { PreparedDocument } from './prepareDocument'

function prepared(href: string, body: string): PreparedDocument {
    const doc = new DOMParser().parseFromString(`<html><body>${body}</body></html>`, 'text/html')
    return { href, doc, styles: [], rootAttributes: [], textLength: 0 }
}

function slice(
    href: string,
    start: number[] | null = null,
    end: number[] | null = null
): ChapterSlice {
    return { href, spineIndex: 0, start, end }
}

function chapter(...slices: ChapterSlice[]): BookChapter {
    return {
        index: 0,
        title: '',
        target: { href: slices[0]!.href, fragment: '' },
        start: { spineIndex: 0, path: [] },
        end: null,
        slices,
    }
}

const images = (n: number, prefix = 'i') =>
    Array.from({ length: n }, (_, i) => `<img src="${prefix}${i}.png">`).join('')

let started: FakeImage[] = []
let pending: Array<() => void> = []

class FakeImage {
    onload: (() => void) | null = null
    onerror: (() => void) | null = null
    order: string[] = []
    set sizes(value: string) {
        this.order.push(`sizes=${value}`)
    }
    set srcset(value: string) {
        this.order.push(`srcset=${value}`)
    }
    set src(value: string) {
        this.order.push(`src=${value}`)
        started.push(this)
        pending.push(() => this.onload?.())
    }
}

async function flush() {
    for (let i = 0; i < 10; i++) await Promise.resolve()
}

async function finishOne() {
    pending.shift()!()
    await flush()
}

beforeEach(() => {
    started = []
    pending = []
    vi.useFakeTimers()
    vi.stubGlobal('Image', FakeImage)
    vi.stubGlobal('requestIdleCallback', undefined)
})

afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
})

describe('slice images', () => {
    it('takes images and SVG images inside the slice range, skipping data URLs', () => {
        const doc = prepared(
            'a.xhtml',
            `<p><img src="before.png"></p>
            <p><img src="data:image/png;base64,AA"><img srcset="x.png 2x" sizes="50vw"></p>
            <svg><image href="svg.png"></image></svg>
            <p><img src="after.png"></p>`
        )
        // Body child nodes: p, text, p, text, svg, text, p.
        const found = sliceImages(doc, slice('a.xhtml', [2], [6]))
        expect(found).toEqual([
            { src: null, srcset: 'x.png 2x', sizes: '50vw' },
            { src: 'svg.png', srcset: null, sizes: null },
        ])
    })
})

describe('chapter prefetch', () => {
    it('prepares the neighbours when idle and warms at most 8 images, 3 at a time', async () => {
        const docs: Record<string, PreparedDocument> = {
            'next.xhtml': prepared('next.xhtml', images(10)),
            'prev.xhtml': prepared('prev.xhtml', ''),
        }
        const prepare = vi.fn(async (href: string) => docs[href]!)
        const prefetch = createChapterPrefetch(prepare)
        prefetch.schedule(
            chapter(slice('next.xhtml'), slice('next2.xhtml')),
            chapter(slice('prev0.xhtml'), slice('prev.xhtml')),
            () => true
        )
        expect(prepare).not.toHaveBeenCalled()
        await vi.advanceTimersByTimeAsync(300)
        await flush()

        expect(prepare.mock.calls.map(call => call[0])).toEqual(['next.xhtml', 'prev.xhtml'])
        expect(started).toHaveLength(3)
        while (pending.length) await finishOne()
        expect(started.map(img => img.order.at(-1))).toEqual(
            Array.from({ length: 8 }, (_, i) => `src=i${i}.png`)
        )
    })

    it('sets sizes, then srcset, then src', async () => {
        const doc = prepared('n.xhtml', '<img src="a.png" srcset="a2.png 2x" sizes="30vw">')
        const prefetch = createChapterPrefetch(async () => doc)
        prefetch.schedule(chapter(slice('n.xhtml')), null, () => true)
        await vi.advanceTimersByTimeAsync(300)
        await flush()
        expect(started[0]!.order).toEqual(['sizes=30vw', 'srcset=a2.png 2x', 'src=a.png'])
    })

    it('issues nothing more once the navigation has moved on', async () => {
        let current = true
        const prefetch = createChapterPrefetch(async href => prepared(href, images(10)))
        prefetch.schedule(chapter(slice('n.xhtml')), null, () => current)
        await vi.advanceTimersByTimeAsync(300)
        await flush()
        expect(started).toHaveLength(3)
        current = false
        while (pending.length) await finishOne()
        expect(started).toHaveLength(3)
    })

    it('skips a job the navigation left before it ran', async () => {
        const prepare = vi.fn(async (href: string) => prepared(href, ''))
        const prefetch = createChapterPrefetch(prepare)
        prefetch.schedule(chapter(slice('n.xhtml')), null, () => false)
        await vi.advanceTimersByTimeAsync(300)
        prefetch.schedule(chapter(slice('m.xhtml')), null, () => true)
        prefetch.cancel()
        await vi.advanceTimersByTimeAsync(300)
        expect(prepare).not.toHaveBeenCalled()
    })
})
