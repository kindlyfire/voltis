import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Router } from 'vue-router'
import { contentApi } from '@/utils/api/content'
import type { BookLocator, BookStructure, Content, UserToContent } from '@/utils/api/types'
import type { BookEntry } from './bookEntry'
import {
    createBookSession,
    type BookAnchor,
    type BookNav,
    type BookSession,
} from './createBookSession'
import { createBookNav } from './useBookDisplayStore'

vi.mock('@/utils/api/content', () => ({
    contentApi: {
        get: vi.fn(),
        bookStructure: vi.fn(),
        bookDocument: vi.fn(),
        updateUserData: vi.fn(),
    },
}))

const STRUCTURE: BookStructure = {
    spine: [
        { href: 'book.xhtml', title: 'Book', linear: true, words: 300 },
        { href: 'notes.xhtml', title: 'Notes', linear: false, words: 100 },
    ],
    toc: [
        { id: 'c1', title: 'One', depth: 0, href: 'book.xhtml', fragment: '' },
        { id: 'c2', title: 'Two', depth: 0, href: 'book.xhtml', fragment: 'c2' },
        { id: 'c3', title: 'Three', depth: 0, href: 'book.xhtml', fragment: 'c3' },
    ],
}

const DOCUMENTS: Record<string, string> = {
    'book.xhtml': `<html><body>
        <p id="p1">chapter one <a href="book.xhtml#p1b">text</a></p>
        <p id="p1b">more of chapter one</p>
        <p><a href="book.xhtml#legacy">to the old anchor</a></p>
        <p><a href="notes.xhtml#n">footnote</a></p>
        <h2 id="c2">Two</h2>
        <p id="p2">chapter two text</p>
        <h2 id="c3">Three</h2>
        <p id="p3">chapter three text</p>
        <p><a name="legacy">legacy anchor</a></p>
    </body></html>`,
    'notes.xhtml': `<html><body><p id="n">a note</p></body></html>`,
    'extra.xhtml': `<html><body><p id="x">off spine</p></body></html>`,
}

function content(progress: Record<string, unknown> = {}, status = 'reading'): Content {
    return {
        id: 'c_1',
        file_mtime: '2026-01-01',
        title: 'A Book',
        type: 'book',
        user_data: { starred: false, status, notes: null, rating: null, progress } as UserToContent,
    } as unknown as Content
}

/** A router and a history stack, so Back/Forward and canonicalization are
 * exercised the way the store wires them. */
class FakeNav implements BookNav {
    entries: Array<{ target: BookAnchor; locator: BookLocator | null }> = []
    index = -1
    session: BookSession | null = null

    push(target: BookAnchor) {
        this.entries.splice(this.index + 1)
        this.entries.push({ target, locator: null })
        this.index++
        this.deliver()
    }

    replace(target: BookAnchor) {
        if (this.index < 0) {
            this.entries.push({ target, locator: null })
            this.index = 0
        } else {
            this.entries[this.index]!.target = target
        }
        this.deliver()
    }

    saveLocator(locator: BookLocator) {
        if (this.index < 0) return
        this.entries[this.index]!.locator = locator
    }

    historyLocator(): BookLocator | null {
        return this.entries[this.index]?.locator ?? null
    }

    go(delta: number) {
        const next = this.index + delta
        if (next < 0 || next >= this.entries.length) return
        this.index = next
        this.deliver()
    }

    private deliver() {
        const target = this.entries[this.index]!.target
        this.session?.setEntry({ ch: target.href, frag: target.fragment || null })
    }
}

/** Stacked 100px blocks, so "first visible block" and sentinel visibility are
 * real geometry rather than one rectangle for everything. */
let scrollY = 0
const BLOCK_HEIGHT = 100

const SLICE_HEIGHT = 1000

/** Text runs in 20px lines of `charsPerLine` characters from each block's
 * top, with the glyph box inset inside its line box and no block margins.
 * Leading and repeated whitespace collapses, `[hidden]` content and zero-width
 * spaces have no box at all, and `<sub>` glyphs sit `SUB_DROP` lower. */
const LINE_HEIGHT = 20
const CHARS_PER_LINE = 10
const GLYPH_INSET = 6
const GLYPH_HEIGHT = 8
const SUB_DROP = 14
let charsPerLine = CHARS_PER_LINE

/** Where a restore puts the first glyph of `line` in the block at `blockTop`. */
function restoredAt(blockTop: number, line = 0) {
    return blockTop + line * LINE_HEIGHT + GLYPH_INSET - 8
}

function topOf(el: Element): number {
    const body = el.closest('body')
    if (!body) return 0
    const host = (body.getRootNode() as ShadowRoot).host as HTMLElement | undefined
    const siblings = host?.parentElement ? Array.from(host.parentElement.children) : []
    const base = Math.max(0, siblings.indexOf(host!)) * SLICE_HEIGHT
    const blocks = Array.from(body.children)
    for (let i = 0; i < blocks.length; i++) {
        if (blocks[i] === el || blocks[i]!.contains(el)) return base + i * BLOCK_HEIGHT
    }
    return base
}

function rectFor(el: Element): DOMRect {
    if (el.closest('[hidden]')) return EMPTY
    const top = topOf(el) - scrollY
    return {
        top,
        bottom: top + BLOCK_HEIGHT,
        left: 0,
        right: 600,
        width: 600,
        height: BLOCK_HEIGHT,
        x: 0,
        y: top,
        toJSON: () => ({}),
    } as DOMRect
}

const EMPTY = {
    top: 0,
    bottom: 0,
    left: 0,
    right: 0,
    width: 0,
    height: 0,
    x: 0,
    y: 0,
    toJSON: () => ({}),
} as DOMRect

function textNodes(root: Node): Text[] {
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
    const out: Text[] = []
    for (let node = walker.nextNode(); node; node = walker.nextNode()) out.push(node as Text)
    return out
}

function glyphRect(text: Text, index: number): DOMRect {
    const parent = text.parentElement
    const body = parent?.closest('body')
    if (!parent || !body || parent === body) return EMPTY
    const block = Array.from(body.children).find(child => child.contains(parent))!
    let visible = 0
    let previous = ''
    for (const node of textNodes(block)) {
        const hidden = !!node.parentElement!.closest('[hidden]')
        const drop = node.parentElement!.closest('sub') ? SUB_DROP : 0
        for (let i = 0; i < node.data.length; i++) {
            const ch = node.data[i]!
            const shown =
                !hidden && ch !== '\u200b' && !(/\s/.test(ch) && (!previous || /\s/.test(previous)))
            if (node === text && i === index) {
                if (!shown) return EMPTY
                const top =
                    topOf(block) -
                    scrollY +
                    Math.floor(visible / charsPerLine) * LINE_HEIGHT +
                    GLYPH_INSET +
                    drop
                const left = (visible % charsPerLine) * 10
                return {
                    ...EMPTY,
                    top,
                    bottom: top + GLYPH_HEIGHT,
                    left,
                    right: left + 10,
                    width: 10,
                    height: GLYPH_HEIGHT,
                    x: left,
                    y: top,
                }
            }
            if (shown) {
                visible++
                previous = ch
            }
        }
    }
    return EMPTY
}

function rangeRect(range: Range): DOMRect {
    const text = range.startContainer as Text
    const rects = []
    for (let i = range.startOffset; i < range.endOffset; i++) {
        const rect = glyphRect(text, i)
        if (rect.width || rect.height) rects.push(rect)
    }
    if (!rects.length) return EMPTY
    const top = Math.min(...rects.map(rect => rect.top))
    const bottom = Math.max(...rects.map(rect => rect.bottom))
    return { ...EMPTY, top, bottom, right: 100, width: 100, height: bottom - top, y: top }
}

function scrollTo(y: number) {
    scrollY = y
    window.dispatchEvent(new Event('scroll'))
}

function deferred<T>() {
    let resolve!: (value: T) => void
    let reject!: (reason?: unknown) => void
    const promise = new Promise<T>((res, rej) => {
        resolve = res
        reject = rej
    })
    return { promise, resolve, reject }
}

async function flush(times = 30) {
    for (let i = 0; i < times; i++) await Promise.resolve()
}

function mount() {
    const host = document.createElement('div')
    const sentinel = document.createElement('div')
    document.body.replaceChildren(host, sentinel)
    return { host, sentinel }
}

function start(entry: Partial<BookEntry> = {}, nav = new FakeNav()) {
    const { host, sentinel } = mount()
    const session = createBookSession('c_1', { ch: null, frag: null, ...entry }, nav)
    nav.session = session
    session.setElements({ host, sentinel })
    return { session, nav, host, sentinel }
}

function bodyOf(host: HTMLElement, index = 0): Element {
    return (host.children[index] as HTMLElement).shadowRoot!.querySelector('body')!
}

function writes() {
    return vi.mocked(contentApi.updateUserData).mock.calls
}

const computedStyle = window.getComputedStyle

beforeEach(() => {
    scrollY = 0
    charsPerLine = CHARS_PER_LINE
    Object.defineProperty(window, 'innerHeight', { value: 768, configurable: true })
    vi.spyOn(window, 'getComputedStyle').mockImplementation((el, pseudo) => {
        const style = computedStyle(el, pseudo)
        return style.writingMode ? style : Object.assign(style, { writingMode: 'horizontal-tb' })
    })
    vi.mocked(contentApi.get).mockResolvedValue(content({ current_page: 7 }))
    vi.mocked(contentApi.bookStructure).mockResolvedValue(STRUCTURE)
    vi.mocked(contentApi.bookDocument).mockImplementation(async (_id, href) => {
        const html = DOCUMENTS[href]
        if (!html) throw new Error('404')
        return html
    })
    vi.mocked(contentApi.updateUserData).mockResolvedValue({ progress: {} } as UserToContent)
    window.scrollTo = ((options: { top: number } | number) => {
        scrollY = typeof options === 'number' ? options : options.top
    }) as typeof window.scrollTo
    Object.defineProperty(window, 'scrollY', { get: () => scrollY, configurable: true })
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (
        this: Element
    ) {
        return rectFor(this)
    })
    Object.defineProperty(Range.prototype, 'getBoundingClientRect', {
        value(this: Range) {
            return rangeRect(this)
        },
        configurable: true,
    })
})

afterEach(() => {
    vi.useRealTimers()
})

describe('mounting', () => {
    it('mounts each slice into its own shadow root, keeping html and body', async () => {
        const { session, host } = start()
        await flush()

        expect(session.pages).toHaveLength(3)
        expect(host.children).toHaveLength(1)
        const shadow = (host.children[0] as HTMLElement).shadowRoot!
        expect(shadow.querySelector('html > body')).not.toBeNull()
        expect(bodyOf(host).textContent).toContain('chapter one text')
        expect(bodyOf(host).textContent).not.toContain('chapter two text')
        await session.dispose()
    })

    it('waits for a host that arrives after the fetches', async () => {
        const nav = new FakeNav()
        const host = document.createElement('div')
        const sentinel = document.createElement('div')
        document.body.replaceChildren(host, sentinel)
        const session = createBookSession('c_1', { ch: null, frag: null }, nav)
        nav.session = session
        await flush()
        expect(host.children).toHaveLength(0)

        session.setElements({ host, sentinel })
        await flush()
        expect(host.children).toHaveLength(1)
        await session.dispose()
    })

    it('re-attaches mounted slices when the host element is replaced', async () => {
        const { session, host } = start()
        await flush()
        const replacement = document.createElement('div')
        session.setElements({ host: replacement, sentinel: null })
        expect(replacement.children).toHaveLength(1)
        expect(host.children).toHaveLength(0)
        await session.dispose()
    })
})

describe('entry resolution', () => {
    it('resumes from a saved locator when the URL names no chapter', async () => {
        const locator: BookLocator = { version: 1, href: 'book.xhtml', textOffset: 60 }
        vi.mocked(contentApi.get).mockResolvedValue(content({ book: locator }))
        const { session, nav } = start()
        await flush()
        expect(session.pageIndex).toBe(1)
        // And the URL is canonicalized onto the page it landed on.
        expect(nav.entries[nav.index]!.target).toEqual({ href: 'book.xhtml', fragment: 'c2' })
        await session.dispose()
    })

    it('lets a deep link to another page override the saved locator', async () => {
        const locator: BookLocator = { version: 1, href: 'book.xhtml', textOffset: 60 }
        vi.mocked(contentApi.get).mockResolvedValue(content({ book: locator }))
        const { session } = start({ ch: 'book.xhtml' })
        await flush()
        expect(session.pageIndex).toBe(0)
        await session.dispose()
    })

    it('recovers a stale locator through its anchor', async () => {
        const locator: BookLocator = {
            version: 1,
            href: 'book.xhtml',
            textOffset: 99_999,
            anchorId: 'c3',
        }
        vi.mocked(contentApi.get).mockResolvedValue(content({ book: locator }))
        const { session } = start()
        await flush()
        expect(session.pageIndex).toBe(2)
        await session.dispose()
    })
})

describe('navigation races', () => {
    const THREE: BookStructure = {
        spine: [
            { href: 'a.xhtml', title: 'A', linear: true, words: 10 },
            { href: 'b.xhtml', title: 'B', linear: true, words: 10 },
            { href: 'c.xhtml', title: 'C', linear: true, words: 10 },
        ],
        toc: [
            { id: 'a', title: 'A', depth: 0, href: 'a.xhtml', fragment: '' },
            { id: 'b', title: 'B', depth: 0, href: 'b.xhtml', fragment: '' },
            { id: 'c', title: 'C', depth: 0, href: 'c.xhtml', fragment: '' },
        ],
    }

    it('keeps the last requested page when an earlier fetch resolves late', async () => {
        const gates: Record<string, ReturnType<typeof deferred<string>>> = {
            'a.xhtml': deferred<string>(),
            'b.xhtml': deferred<string>(),
            'c.xhtml': deferred<string>(),
        }
        vi.mocked(contentApi.bookStructure).mockResolvedValue(THREE)
        vi.mocked(contentApi.bookDocument).mockImplementation((_id, href) => gates[href]!.promise)

        const { session, host, nav } = start()
        gates['a.xhtml']!.resolve('<html><body><p>first</p></body></html>')
        await flush()
        expect(session.pageIndex).toBe(0)

        session.goToPage(1)
        await flush()
        session.goToPage(2)
        await flush()

        // Both destinations are still in flight; the abandoned one lands first.
        gates['c.xhtml']!.resolve('<html><body><p>third</p></body></html>')
        await flush()
        gates['b.xhtml']!.resolve('<html><body><p>second</p></body></html>')
        await flush()

        expect(session.pageIndex).toBe(2)
        expect(bodyOf(host).textContent).toContain('third')
        expect(bodyOf(host).textContent).not.toContain('second')
        expect(nav.entries[nav.index]!.target.href).toBe('c.xhtml')
        await session.dispose()
    })

    it('applies a route change that arrives while the book is still loading', async () => {
        const gate = deferred<BookStructure>()
        vi.mocked(contentApi.bookStructure).mockReturnValue(gate.promise)

        const { session } = start()
        await flush()
        expect(session.loading).toBe(true)

        session.setEntry({ ch: 'book.xhtml', frag: 'c3' })
        gate.resolve(STRUCTURE)
        await flush()

        expect(session.loading).toBe(false)
        expect(session.pages).toHaveLength(3)
        expect(session.pageIndex).toBe(2)
        await session.dispose()
    })

    it('finishes restoration when a mounted link is followed mid-settlement', async () => {
        vi.spyOn(HTMLImageElement.prototype, 'complete', 'get').mockReturnValue(false)
        vi.mocked(contentApi.bookDocument).mockImplementation(async (_id, href) =>
            href === 'book.xhtml'
                ? DOCUMENTS[href]!.replace('<p id="p1">', '<p id="p1"><img src="a.png" />')
                : DOCUMENTS[href]!
        )

        const { session, host } = start()
        await flush()
        // The image never loads, so the restore is still waiting to settle.
        expect(session.restoring).toBe(true)

        const link = bodyOf(host).querySelector('[data-book-frag="p1b"]') as HTMLElement
        link.click()
        await flush()

        expect(session.restoring).toBe(false)
        expect(scrollY).toBe(BLOCK_HEIGHT - 8)
        await session.dispose()
    })

    it('does not let a disposed session rewrite the route', async () => {
        const { session, nav } = start()
        const seen = nav.entries.length
        void session.dispose()
        await flush()
        expect(nav.entries.length).toBe(seen)
    })
})

describe('unreachable documents', () => {
    it('keeps the mounted page when a linked document cannot be loaded', async () => {
        vi.mocked(contentApi.bookStructure).mockResolvedValue({
            spine: [
                { href: 'book.xhtml', title: 'Book', linear: true, words: 10 },
                { href: 'gone.xhtml', title: 'Gone', linear: true, words: 10 },
            ],
            toc: [
                { id: 'c1', title: 'One', depth: 0, href: 'book.xhtml', fragment: '' },
                { id: 'c2', title: 'Gone', depth: 0, href: 'gone.xhtml', fragment: '' },
            ],
        })
        vi.mocked(contentApi.bookDocument).mockImplementation(async (_id, href) => {
            if (href === 'gone.xhtml') throw new Error('404')
            return `<html><body><p id="p1">here</p> <p><a href="gone.xhtml">broken</a></p></body></html>`
        })

        const { session, host } = start()
        await flush()
        expect(session.pageIndex).toBe(0)

        const link = bodyOf(host).querySelector('[data-book-href="gone.xhtml"]') as HTMLElement
        link.click()
        await flush()

        expect(session.error).toBeNull()
        expect(session.notice).toContain('missing')
        expect(session.pageIndex).toBe(0)
        expect(bodyOf(host).textContent).toContain('here')
        await session.dispose()
    })
})

describe('progress', () => {
    it('merges the block the reader scrolled to into the existing progress', async () => {
        const { session } = start()
        await flush()
        scrollTo(150)
        await flush()
        await session.dispose()

        expect(writes()).toHaveLength(1)
        const [, payload, init] = writes()[0]!
        expect(payload.progress!.current_page).toBe(7)
        expect(payload.progress!.book).toMatchObject({
            version: 1,
            href: 'book.xhtml',
            anchorId: 'p1b',
        })
        expect(payload.progress!.book!.textOffset).toBeGreaterThan(0)
        expect(payload.progress!.progress_percent).toBeGreaterThanOrEqual(0)
        expect(payload.status).toBe('reading')
        expect(init).toEqual({ keepalive: true })
    })

    it('flushes with keepalive when the tab is hidden, and resumes afterwards', async () => {
        const { session } = start()
        await flush()
        Object.defineProperty(document, 'visibilityState', {
            value: 'hidden',
            configurable: true,
        })
        document.dispatchEvent(new Event('visibilitychange'))
        await flush()
        expect(writes()).toHaveLength(1)
        expect(writes()[0]![2]).toEqual({ keepalive: true })

        Object.defineProperty(document, 'visibilityState', {
            value: 'visible',
            configurable: true,
        })
        document.dispatchEvent(new Event('visibilitychange'))
        scrollTo(150)
        await flush()
        await session.dispose()
        expect(writes().length).toBeGreaterThan(1)
    })

    it('keeps a non-reading status untouched', async () => {
        vi.mocked(contentApi.get).mockResolvedValue(content({}, 'dropped'))
        const { session } = start()
        await flush()
        await session.dispose()
        expect(writes()[0]![1].status).toBeUndefined()
    })
})

describe('stale books without word counts', () => {
    // Nothing backfills `words`, so every book scanned before the change
    // reports 0 for every document.
    it('still reports a percent from equal per-document weights', async () => {
        vi.mocked(contentApi.bookStructure).mockResolvedValue({
            spine: STRUCTURE.spine.map(item => ({ ...item, words: 0 })),
            toc: STRUCTURE.toc,
        })
        const { session } = start()
        await flush()
        const initial = session.percent

        scrollTo(150)
        await flush()
        await session.dispose()

        const percent = writes().at(-1)![1].progress!.progress_percent!
        expect(percent).toBeGreaterThan(initial)
        expect(percent).toBeLessThanOrEqual(100)
    })
})

describe('completion', () => {
    it('completes only after the reader produces input on the final page', async () => {
        const { session } = start()
        await flush()
        session.goToPage(2)
        await flush()
        expect(session.pageIndex).toBe(2)

        scrollTo(10)
        await flush()
        expect(writes().some(([, payload]) => payload.status === 'completed')).toBe(false)

        window.dispatchEvent(new Event('pointerdown'))
        await flush()
        await session.dispose()
        expect(writes().at(-1)![1].status).toBe('completed')
    })

    it('does not complete when the sentinel is above the viewport', async () => {
        const { session } = start()
        await flush()
        session.goToPage(2)
        await flush()
        scrollTo(5000)
        window.dispatchEvent(new Event('pointerdown'))
        await flush()
        await session.dispose()
        expect(writes().every(([, payload]) => payload.status !== 'completed')).toBe(true)
    })

    // On the last page, where everything but the standalone guard says complete.
    it('never completes a book from standalone viewing', async () => {
        const { session, host } = start()
        await flush()
        session.goToPage(2)
        await flush()
        scrollTo(10)
        await flush()

        const body = bodyOf(host)
        const link = body.ownerDocument.createElement('a')
        link.setAttribute('data-book-href', 'notes.xhtml')
        link.setAttribute('data-book-frag', '')
        body.append(link)
        link.click()
        await flush()
        expect(session.standalone).not.toBeNull()

        window.dispatchEvent(new Event('pointerdown'))
        await flush()
        await session.dispose()
        expect(writes().length).toBeGreaterThan(0)
        expect(writes().every(([, payload]) => payload.status !== 'completed')).toBe(true)
    })
})

describe('standalone documents', () => {
    it('returns to the passage it was opened from', async () => {
        const { session, host } = start()
        await flush()
        scrollTo(150)
        await flush()

        const link = bodyOf(host).querySelector('[data-book-href="notes.xhtml"]') as HTMLElement
        link.click()
        await flush()
        expect(session.standalone).toEqual({ href: 'notes.xhtml', title: 'Notes' })
        expect(bodyOf(host).textContent).toContain('a note')

        session.closeStandalone()
        await flush()
        expect(session.standalone).toBeNull()
        expect(session.pageIndex).toBe(0)
        expect(scrollY).toBe(restoredAt(2 * BLOCK_HEIGHT))
        await session.dispose()
    })

    it('never writes a standalone document into progress', async () => {
        const { session, host } = start()
        await flush()
        scrollTo(150)
        await flush()
        expect(writes()).toHaveLength(0)

        const link = bodyOf(host).querySelector('[data-book-href="notes.xhtml"]') as HTMLElement
        link.click()
        await flush()

        // Leaving the flow persists the outgoing passage there and then.
        expect(writes()).toHaveLength(1)
        expect(writes()[0]![1].progress!.book).toMatchObject({
            href: 'book.xhtml',
            anchorId: 'p1b',
        })

        scrollTo(50)
        await flush()
        await session.dispose()

        expect(writes().every(([, payload]) => payload.progress!.book!.href === 'book.xhtml')).toBe(
            true
        )
    })

    it('opens an off-spine document and reports one that is missing', async () => {
        const { session } = start({ ch: 'extra.xhtml' })
        await flush()
        expect(session.standalone?.href).toBe('extra.xhtml')
        await session.dispose()

        const gone = start({ ch: 'nowhere.xhtml' })
        await flush()
        expect(gone.session.standalone).toBeNull()
        expect(gone.session.notice).toContain('unavailable')
        expect(gone.session.pageIndex).toBe(0)
        await gone.session.dispose()
    })
})

describe('internal links', () => {
    it('follows a legacy name anchor onto its own page', async () => {
        const { session, host } = start()
        await flush()
        const link = bodyOf(host).querySelector('[data-book-frag="legacy"]') as HTMLElement
        link.click()
        await flush()

        expect(session.pageIndex).toBe(2)
        expect(scrollY).toBe(2 * BLOCK_HEIGHT - 8)
        expect(session.notice).toBeNull()
        await session.dispose()
    })

    it('reports a link whose fragment no longer exists', async () => {
        const { session, host } = start()
        await flush()
        const body = bodyOf(host)
        const link = body.ownerDocument.createElement('a')
        link.setAttribute('data-book-href', 'book.xhtml')
        link.setAttribute('data-book-frag', 'vanished')
        body.append(link)
        link.click()
        await flush()

        expect(session.notice).toContain('unavailable')
        await session.dispose()
    })
})

describe('history', () => {
    it('returns to the passage left behind on Back, and to the new one on Forward', async () => {
        vi.useFakeTimers()
        const { session, nav } = start()
        await flush()
        // Past the frame in which the mount scrolled, so the scroll below
        // reads as the reader's own.
        await vi.advanceTimersByTimeAsync(50)

        scrollTo(150)
        await vi.advanceTimersByTimeAsync(600)
        expect(nav.entries[0]!.locator?.anchorId).toBe('p1b')

        session.goToPage(2)
        await vi.advanceTimersByTimeAsync(300)
        expect(session.pageIndex).toBe(2)

        // Read on at the destination, then leave and come back both ways.
        await vi.advanceTimersByTimeAsync(50)
        scrollTo(restoredAt(BLOCK_HEIGHT))
        await vi.advanceTimersByTimeAsync(600)
        expect(nav.entries[1]!.locator?.anchorId).toBe('p3')

        nav.go(-1)
        await vi.advanceTimersByTimeAsync(300)
        await flush()
        expect(session.pageIndex).toBe(0)
        expect(scrollY).toBe(restoredAt(2 * BLOCK_HEIGHT))

        nav.go(1)
        await vi.advanceTimersByTimeAsync(300)
        await flush()
        expect(session.pageIndex).toBe(2)
        expect(scrollY).toBe(restoredAt(BLOCK_HEIGHT))
        await session.dispose()
    })
})

// The adapter the store really installs, rather than the test's stand-in.
describe('history adapter', () => {
    const router = { push: vi.fn(), replace: vi.fn() } as unknown as Router
    const locator: BookLocator = { version: 1, href: 'book.xhtml', textOffset: 42 }

    it('scopes a snapshot to its book and leaves router state alone', () => {
        history.replaceState({ position: 3 }, '')
        const mine = createBookNav(router, 'c_1')
        const other = createBookNav(router, 'c_other')

        mine.saveLocator(locator)
        expect(mine.historyLocator()).toEqual(locator)
        expect(other.historyLocator()).toBeNull()
        expect((history.state as { position?: number }).position).toBe(3)

        other.saveLocator({ ...locator, textOffset: 7 })
        expect(mine.historyLocator()).toBeNull()
        expect(other.historyLocator()).toEqual({ ...locator, textOffset: 7 })
    })

    it('rejects a malformed snapshot', () => {
        history.replaceState(
            { bookLocator: { contentId: 'c_1', locator: { version: 2, href: 'x' } } },
            ''
        )
        expect(createBookNav(router, 'c_1').historyLocator()).toBeNull()
    })
})

describe('paging backwards', () => {
    it('lands at the bottom of the page it arrives at', async () => {
        const { session } = start()
        await flush()
        session.goToPage(1)
        await flush()
        expect(session.pageIndex).toBe(1)

        Object.defineProperty(document.documentElement, 'scrollHeight', {
            value: 4000,
            configurable: true,
        })
        session.goToPage(0, true)
        await flush()

        expect(session.pageIndex).toBe(0)
        expect(scrollY).toBe(4000)

        // The landing is spent, so the next arrival is positioned normally.
        session.goToPage(1)
        await flush()
        expect(scrollY).toBe(0)

        Reflect.deleteProperty(document.documentElement, 'scrollHeight')
        await session.dispose()
    })

    it('drops the landing when the turn is overtaken by another entry', async () => {
        const { session } = start()
        await flush()
        session.goToPage(1)
        await flush()

        Object.defineProperty(document.documentElement, 'scrollHeight', {
            value: 4000,
            configurable: true,
        })
        session.goToPage(0, true)
        // Same page, but a deliberate anchor: it must not inherit the landing.
        session.setEntry({ ch: 'book.xhtml', frag: 'p1b' })
        await flush()

        expect(session.pageIndex).toBe(0)
        expect(scrollY).toBe(BLOCK_HEIGHT - 8)

        Reflect.deleteProperty(document.documentElement, 'scrollHeight')
        await session.dispose()
    })
})

/** Five paragraphs of exactly five lines each, the third indented and the
 * fourth opening with hidden text. */
const PARA = 'abcdefghi '.repeat(5)
const LONG_BODY = `
    <p id="l0">${PARA}</p>
    <p id="l1">${PARA}</p>
    <p id="l2">
        ${PARA}</p>
    <p id="l3"><span hidden="">PAGEBREAK</span>${PARA}</p>
    <p id="l4">${PARA}</p>`

function withBook(body: string, locator?: Omit<BookLocator, 'version' | 'href'>) {
    vi.mocked(contentApi.bookStructure).mockResolvedValue({
        spine: [{ href: 'long.xhtml', title: 'Long', linear: true, words: 50 }],
        toc: [{ id: 'l', title: 'Long', depth: 0, href: 'long.xhtml', fragment: '' }],
    })
    vi.mocked(contentApi.bookDocument).mockResolvedValue(`<html><body>${body}</body></html>`)
    if (locator) {
        vi.mocked(contentApi.get).mockResolvedValue(
            content({ book: { version: 1, href: 'long.xhtml', ...locator } })
        )
    }
}

async function startTimed() {
    vi.useFakeTimers()
    const started = start()
    await vi.advanceTimersByTimeAsync(50)
    await flush()
    return started
}

/** Each round rescrolls in place, so the next reflow captures afresh instead
 * of reusing its anchor. */
async function expectStableReflows(session: BookSession, y: number) {
    for (let i = 0; i < 3; i++) {
        session.reflow()
        await vi.advanceTimersByTimeAsync(300)
        expect(scrollY).toBe(y)
        scrollTo(scrollY)
        await vi.advanceTimersByTimeAsync(300)
    }
}

describe('reflow', () => {
    it('restores the passage captured before the layout change', async () => {
        const { session } = await startTimed()
        scrollTo(150)
        await vi.advanceTimersByTimeAsync(300)
        const before = session.percent

        session.reflow()
        scrollY = 0
        await vi.advanceTimersByTimeAsync(300)

        expect(scrollY).toBe(restoredAt(2 * BLOCK_HEIGHT))
        expect(session.percent).toBe(before)
        await session.dispose()
    })

    it('leaves window resizes to native scroll anchoring', async () => {
        const { session } = await startTimed()
        scrollTo(150)
        await vi.advanceTimersByTimeAsync(300)

        window.dispatchEvent(new Event('resize'))
        await vi.advanceTimersByTimeAsync(300)

        expect(scrollY).toBe(150)
        await session.dispose()
    })

    it('recaptures a mid-paragraph line where it was restored', async () => {
        withBook(LONG_BODY)
        const { session } = await startTimed()
        scrollTo(restoredAt(BLOCK_HEIGHT, 2) - 5)
        await vi.advanceTimersByTimeAsync(300)
        await expectStableReflows(session, restoredAt(BLOCK_HEIGHT, 2))
        await session.dispose()
    })

    it('does not drift into a block whose last line straddles the top', async () => {
        withBook(LONG_BODY)
        const { session } = await startTimed()
        scrollTo(2 * BLOCK_HEIGHT - 10)
        await vi.advanceTimersByTimeAsync(300)
        await expectStableReflows(session, restoredAt(2 * BLOCK_HEIGHT))
        await session.dispose()
    })

    it('does not drift onto lower glyphs of the line above', async () => {
        const line = 'abcd<sub>e</sub>fghi '.repeat(5)
        withBook(`<p id="s0">${line}</p> <p id="s1">${line}</p>`)
        const { session } = await startTimed()
        scrollTo(restoredAt(BLOCK_HEIGHT, 2))
        await vi.advanceTimersByTimeAsync(300)
        await expectStableReflows(session, restoredAt(BLOCK_HEIGHT, 2))
        await session.dispose()
    })

    it('reuses its anchor, now mid-line, across layout changes until the reader scrolls', async () => {
        withBook(LONG_BODY)
        const { session, nav } = await startTimed()
        scrollTo(restoredAt(BLOCK_HEIGHT, 2) - 5)
        await vi.advanceTimersByTimeAsync(600)
        const anchor = 50 + 2 * CHARS_PER_LINE
        expect(nav.historyLocator()?.textOffset).toBe(anchor)

        session.reflow()
        charsPerLine = 7
        scrollTo(scrollY - 3)
        await vi.advanceTimersByTimeAsync(600)
        expect(scrollY).toBe(restoredAt(BLOCK_HEIGHT, 2))
        expect(nav.historyLocator()?.textOffset).toBe(anchor)

        session.reflow()
        charsPerLine = 6
        await vi.advanceTimersByTimeAsync(600)
        expect(scrollY).toBe(restoredAt(BLOCK_HEIGHT, 3))
        expect(nav.historyLocator()?.textOffset).toBe(anchor)

        scrollTo(scrollY + 1)
        await vi.advanceTimersByTimeAsync(600)
        expect(nav.historyLocator()?.textOffset).toBe(50 + 3 * 6)
        await session.dispose()
    })
})

describe('reflow anchor lifetime', () => {
    const P2 = 'chapter one textmore of chapter oneto the old anchorfootnoteTwo'.length
    const THIRD_BLOCK = 'chapter one text'.length + 'more of chapter one'.length

    async function reflowed() {
        const started = await startTimed()
        scrollTo(150)
        await vi.advanceTimersByTimeAsync(600)
        started.session.reflow()
        await vi.advanceTimersByTimeAsync(600)
        expect(started.nav.historyLocator()?.textOffset).toBe(THIRD_BLOCK)
        return started
    }

    it('is dropped by a same-page link', async () => {
        const { session, host, nav } = await reflowed()
        ;(bodyOf(host).querySelector('[data-book-frag="p1b"]') as HTMLElement).click()
        await vi.advanceTimersByTimeAsync(600)

        expect(scrollY).toBe(BLOCK_HEIGHT - 8)
        expect(nav.historyLocator()).toMatchObject({
            textOffset: 'chapter one text'.length,
            anchorId: 'p1b',
        })
        await session.dispose()
    })

    it('is dropped by a navigation', async () => {
        const { session, nav } = await reflowed()
        session.goToPage(1)
        await vi.advanceTimersByTimeAsync(600)
        expect(session.pageIndex).toBe(1)
        expect(nav.historyLocator()).toMatchObject({ textOffset: P2, anchorId: 'p2' })

        nav.go(-1)
        await vi.advanceTimersByTimeAsync(600)
        nav.go(1)
        await vi.advanceTimersByTimeAsync(600)
        expect(session.pageIndex).toBe(1)
        expect(scrollY).toBe(restoredAt(BLOCK_HEIGHT))
        await session.dispose()
    })
})

describe('character positions', () => {
    it('takes the straddling line when no glyph top is inside the viewport', async () => {
        Object.defineProperty(window, 'innerHeight', { value: 20, configurable: true })
        withBook(LONG_BODY)
        const { session } = start()
        await flush()
        scrollTo(restoredAt(BLOCK_HEIGHT, 2) + 5)
        await flush()
        await session.dispose()

        expect(writes().at(-1)![1].progress!.book).toMatchObject({
            textOffset: 50 + 2 * CHARS_PER_LINE,
            anchorId: 'l1',
        })
    })

    it('takes the start of a textless block filling the viewport', async () => {
        Object.defineProperty(window, 'innerHeight', { value: 20, configurable: true })
        withBook(`<p>${PARA}</p> <div id="blank"></div> <p>${PARA}</p>`)
        const { session } = start()
        await flush()
        scrollTo(BLOCK_HEIGHT)
        await flush()
        await session.dispose()

        expect(writes().at(-1)![1].progress!.book).toMatchObject({
            textOffset: 50,
            anchorId: 'blank',
        })
    })

    it('captures the first visible line, not the start of its block', async () => {
        withBook(LONG_BODY)
        const { session } = start()
        await flush()
        scrollTo(restoredAt(BLOCK_HEIGHT, 2) - 5)
        await flush()
        await session.dispose()

        expect(writes().at(-1)![1].progress!.book).toMatchObject({
            textOffset: 50 + 2 * CHARS_PER_LINE,
            anchorId: 'l1',
        })
    })

    it('never captures collapsed indentation or hidden text', async () => {
        withBook(LONG_BODY)
        const { session, nav } = await startTimed()

        scrollTo(restoredAt(2 * BLOCK_HEIGHT))
        await vi.advanceTimersByTimeAsync(600)
        expect(nav.historyLocator()?.textOffset).toBe(101)

        scrollTo(restoredAt(3 * BLOCK_HEIGHT))
        await vi.advanceTimersByTimeAsync(600)
        expect(nav.historyLocator()?.textOffset).toBe(151 + 'PAGEBREAK'.length)
        await session.dispose()
    })

    it('skips a laid-out space and a boxless character opening a line', async () => {
        withBook(`
            <p>${PARA}</p>
            <p>abcdefghij klmnopqrs</p>
            <p>abcdefghijklmno${'\u200b'}pqrstuvwxyzabcd</p>`)
        const { session, nav } = await startTimed()

        scrollTo(restoredAt(BLOCK_HEIGHT, 1))
        await vi.advanceTimersByTimeAsync(600)
        expect(nav.historyLocator()?.textOffset).toBe(50 + 11)

        scrollTo(restoredAt(2 * BLOCK_HEIGHT, 1))
        await vi.advanceTimersByTimeAsync(600)
        expect(nav.historyLocator()?.textOffset).toBe(70 + 10)
        await session.dispose()
    })

    it('restores an old block-start locator to the block first glyph', async () => {
        withBook(LONG_BODY, { textOffset: 100, anchorId: 'l2' })
        const { session } = start()
        await flush()
        expect(scrollY).toBe(restoredAt(2 * BLOCK_HEIGHT))
        await session.dispose()
    })

    it('restores a locator into hidden text to the start of its block', async () => {
        withBook(LONG_BODY, { textOffset: 151, anchorId: 'l3' })
        const { session } = start()
        await flush()
        expect(scrollY).toBe(3 * BLOCK_HEIGHT - 8)
        await session.dispose()
    })

    it('resumes a mid-paragraph locator on its own line', async () => {
        withBook(LONG_BODY, { textOffset: 50 + 3 * CHARS_PER_LINE, anchorId: 'l1' })
        const { session } = start()
        await flush()
        expect(scrollY).toBe(restoredAt(BLOCK_HEIGHT, 3))
        await session.dispose()
    })

    it('falls back to the block start of vertical text past a straddling slice end', async () => {
        vi.mocked(contentApi.bookStructure).mockResolvedValue({
            spine: [
                { href: 'a.xhtml', title: 'A', linear: true, words: 10 },
                { href: 'b.xhtml', title: 'B', linear: true, words: 10 },
            ],
            toc: [{ id: 'c1', title: 'One', depth: 0, href: 'a.xhtml', fragment: '' }],
        })
        vi.mocked(contentApi.bookDocument).mockImplementation(async (_id, href) =>
            href === 'a.xhtml'
                ? `<html><body>${`<p>${PARA}</p>`.repeat(10)}</body></html>`
                : `<html><body><p id="v" style="writing-mode: vertical-rl">
                    ${PARA}</p></body></html>`
        )
        const { session } = start()
        await flush()
        scrollTo(SLICE_HEIGHT - 10)
        await flush()
        await session.dispose()

        expect(writes().at(-1)![1].progress!.book).toMatchObject({
            href: 'b.xhtml',
            textOffset: 0,
            anchorId: 'v',
        })
    })
})

describe('write ordering', () => {
    it('cancels an in-flight write before the closing one', async () => {
        vi.useFakeTimers()
        const hanging = deferred<UserToContent>()
        vi.mocked(contentApi.updateUserData).mockReturnValueOnce(hanging.promise)
        const { session } = start()
        await vi.advanceTimersByTimeAsync(50)
        await flush()

        scrollTo(150)
        await vi.advanceTimersByTimeAsync(1500)
        expect(writes()).toHaveLength(1)

        const inFlight = writes()[0]![2]!.signal!
        expect(inFlight.aborted).toBe(false)

        void session.dispose()
        await flush()

        expect(inFlight.aborted).toBe(true)
        expect(writes()).toHaveLength(2)
        expect(writes()[1]![2]).toEqual({ keepalive: true })
        hanging.resolve({ progress: {} } as UserToContent)
    })
})

describe('textless pages', () => {
    const PLATES: BookStructure = {
        spine: [{ href: 'plates.xhtml', title: 'Plates', linear: true, words: 0 }],
        toc: [
            { id: 'a', title: 'Plate A', depth: 0, href: 'plates.xhtml', fragment: 'a' },
            { id: 'b', title: 'Plate B', depth: 0, href: 'plates.xhtml', fragment: 'b' },
        ],
    }

    function withPlates(anchorId: string) {
        vi.mocked(contentApi.bookStructure).mockResolvedValue(PLATES)
        vi.mocked(contentApi.bookDocument).mockResolvedValue(
            '<html><body><img id="a" src="a.png" /> <img id="b" src="b.png" /></body></html>'
        )
        vi.mocked(contentApi.get).mockResolvedValue(
            content({ book: { version: 1, href: 'plates.xhtml', textOffset: 0, anchorId } })
        )
    }

    // Both plates sit at offset 0, so only the anchor can tell the pages apart.
    it('restores the second plate from its anchor', async () => {
        withPlates('b')
        const { session } = start()
        await flush()
        expect(session.pages).toHaveLength(2)
        expect(session.pageIndex).toBe(1)
        await session.dispose()
    })

    it('restores the first plate from its anchor', async () => {
        withPlates('a')
        const { session } = start()
        await flush()
        expect(session.pageIndex).toBe(0)
        await session.dispose()
    })
})

describe('standalone routing', () => {
    it('puts the reading route back when standalone is closed', async () => {
        const { session, nav } = start({ ch: 'notes.xhtml' })
        await flush()
        expect(session.standalone?.href).toBe('notes.xhtml')

        await session.closeStandalone()
        await flush()
        expect(session.standalone).toBeNull()
        expect(nav.entries[nav.index]!.target.href).toBe('book.xhtml')
        await session.dispose()
    })
})

describe('surviving a failed navigation', () => {
    const WITH_GONE: BookStructure = {
        spine: [
            { href: 'book.xhtml', title: 'Book', linear: true, words: 10 },
            { href: 'gone.xhtml', title: 'Gone', linear: true, words: 10 },
        ],
        toc: [
            { id: 'c1', title: 'One', depth: 0, href: 'book.xhtml', fragment: '' },
            { id: 'c2', title: 'Gone', depth: 0, href: 'gone.xhtml', fragment: '' },
        ],
    }

    function withMissingSecondDocument() {
        vi.mocked(contentApi.bookStructure).mockResolvedValue(WITH_GONE)
        vi.mocked(contentApi.bookDocument).mockImplementation(async (_id, href) => {
            if (href === 'gone.xhtml') throw new Error('404')
            return DOCUMENTS[href]!
        })
    }

    it('keeps capturing on the page it kept after Next fails', async () => {
        withMissingSecondDocument()
        const { session } = start()
        await flush()

        session.goToPage(1)
        await flush()
        expect(session.pageIndex).toBe(0)
        expect(session.error).toBeNull()

        scrollTo(150)
        await flush()
        await session.dispose()

        expect(writes().at(-1)![1].progress!.book).toMatchObject({ anchorId: 'p1b' })
    })

    it('hands the page back when a link fails mid-settlement', async () => {
        withMissingSecondDocument()
        vi.spyOn(HTMLImageElement.prototype, 'complete', 'get').mockReturnValue(false)
        vi.mocked(contentApi.bookDocument).mockImplementation(async (_id, href) => {
            if (href === 'gone.xhtml') throw new Error('404')
            return DOCUMENTS[href]!.replace('<p id="p1">', '<p id="p1"><img src="a.png" />')
        })

        const { session, host } = start()
        await flush()
        expect(session.restoring).toBe(true)

        const body = bodyOf(host)
        const link = body.ownerDocument.createElement('a')
        link.setAttribute('data-book-href', 'gone.xhtml')
        link.setAttribute('data-book-frag', '')
        body.append(link)
        link.click()
        await flush()

        expect(session.restoring).toBe(false)
        expect(session.notice).toContain('missing')

        scrollTo(150)
        await flush()
        await session.dispose()
        expect(writes().at(-1)![1].progress!.book).toMatchObject({ anchorId: 'p1b' })
    })
})

describe('disposal', () => {
    it('cancels deferred capture and repositioning', async () => {
        vi.useFakeTimers()
        const { session, nav } = start()
        await vi.advanceTimersByTimeAsync(50)
        await flush()

        scrollTo(150)
        session.reflow()
        await session.dispose()

        const stamped = JSON.stringify(nav.entries)
        const settled = writes().length
        scrollY = 0
        await vi.advanceTimersByTimeAsync(2000)

        expect(scrollY).toBe(0)
        expect(writes()).toHaveLength(settled)
        expect(JSON.stringify(nav.entries)).toBe(stamped)
    })
})

describe('merged pages', () => {
    const MERGED: BookStructure = {
        spine: [
            { href: 'a.xhtml', title: 'A', linear: true, words: 10 },
            { href: 'b.xhtml', title: 'B', linear: true, words: 10 },
        ],
        toc: [{ id: 'c1', title: 'One', depth: 0, href: 'a.xhtml', fragment: '' }],
    }

    it('resolves a stale anchor inside the locator own document', async () => {
        vi.mocked(contentApi.bookStructure).mockResolvedValue(MERGED)
        vi.mocked(contentApi.bookDocument).mockImplementation(
            async (_id, href) =>
                `<html><body><p id="start">${href} first</p> <p>${href} second</p></body></html>`
        )
        vi.mocked(contentApi.get).mockResolvedValue(
            content({
                book: { version: 1, href: 'b.xhtml', textOffset: 99_999, anchorId: 'start' },
            })
        )

        const { session, host } = start()
        await flush()

        expect(session.pages).toHaveLength(1)
        expect(host.children).toHaveLength(2)
        // The second slice's band, not the identically-anchored first one.
        expect(scrollY).toBe(SLICE_HEIGHT - 8)
        await session.dispose()
    })
})

describe('recovering from a failed Contents selection', () => {
    it('keeps capturing after an unreachable off-spine document', async () => {
        const { session } = start()
        await flush()

        // What a Contents link to a document that is no longer in the archive
        // looks like once the router has moved.
        session.setEntry({ ch: 'nowhere.xhtml', frag: null })
        await flush()
        expect(session.standalone).toBeNull()
        expect(session.notice).toContain('unavailable')
        expect(session.restoring).toBe(false)

        scrollTo(150)
        await flush()
        await session.dispose()
        expect(writes().at(-1)![1].progress!.book).toMatchObject({ anchorId: 'p1b' })
    })

    it('ignores a stale standalone failure while a newer navigation settles', async () => {
        vi.spyOn(HTMLImageElement.prototype, 'complete', 'get').mockReturnValue(false)
        const gate = deferred<string>()
        vi.mocked(contentApi.bookDocument).mockImplementation((_id, href) =>
            href === 'notes.xhtml'
                ? gate.promise
                : // only the later page carries the image that blocks settling
                  Promise.resolve(DOCUMENTS[href]!.replace('<p id="p2">', '<img /><p id="p2">'))
        )

        const { session, host, nav } = start()
        await flush()
        expect(session.restoring).toBe(false)

        const link = bodyOf(host).querySelector('[data-book-href="notes.xhtml"]') as HTMLElement
        link.click()
        await flush()

        session.goToPage(1)
        await flush()
        expect(session.standalone).toBeNull()
        expect(session.restoring).toBe(true)

        const stamped = JSON.stringify(nav.entries)
        gate.reject(new Error('too late'))
        await flush()

        expect(session.restoring).toBe(true)
        expect(JSON.stringify(nav.entries)).toBe(stamped)
        await session.dispose()
    })
})

describe('body as a target', () => {
    it('navigates to a fragment naming a document body', async () => {
        vi.mocked(contentApi.bookStructure).mockResolvedValue({
            spine: [
                { href: 'a.xhtml', title: 'A', linear: true, words: 10 },
                { href: 'b.xhtml', title: 'B', linear: true, words: 10 },
            ],
            toc: [
                { id: 'c1', title: 'One', depth: 0, href: 'a.xhtml', fragment: '' },
                { id: 'c2', title: 'Two', depth: 0, href: 'b.xhtml', fragment: 'chapter' },
            ],
        })
        vi.mocked(contentApi.bookDocument).mockImplementation(async (_id, href) =>
            href === 'b.xhtml'
                ? '<html><body id="chapter"><p>second</p></body></html>'
                : '<html><body><p>first</p></body></html>'
        )

        const { session, host } = start({ ch: 'b.xhtml', frag: 'chapter' })
        await flush()

        expect(session.pages).toHaveLength(2)
        expect(session.pageIndex).toBe(1)
        expect(session.notice).toBeNull()
        expect(bodyOf(host).textContent).toContain('second')
        await session.dispose()
    })
})

describe('atomic page transitions', () => {
    const TWO: BookStructure = {
        spine: [
            { href: 'a.xhtml', title: 'A', linear: true, words: 10 },
            { href: 'b.xhtml', title: 'B', linear: true, words: 10 },
        ],
        toc: [
            { id: 'c1', title: 'One', depth: 0, href: 'a.xhtml', fragment: '' },
            { id: 'c2', title: 'Two', depth: 0, href: 'b.xhtml', fragment: '' },
        ],
    }

    it('does not complete the book after a cancelled move to the final page', async () => {
        const gate = deferred<string>()
        vi.mocked(contentApi.bookStructure).mockResolvedValue(TWO)
        vi.mocked(contentApi.bookDocument).mockImplementation((_id, href) => {
            if (href === 'b.xhtml') return gate.promise
            if (href === 'a.xhtml') {
                return Promise.resolve(
                    `<html><body><p id="p1">first</p> <p><a href="nowhere.xhtml">broken</a></p></body></html>`
                )
            }
            return Promise.reject(new Error('404'))
        })

        const { session, host } = start()
        await flush()
        expect(session.pageIndex).toBe(0)

        session.goToPage(1)
        await flush()

        // The final page never mounts: a link still on screen cancels it, and
        // then fails itself.
        const link = bodyOf(host).querySelector('[data-book-href="nowhere.xhtml"]') as HTMLElement
        link.click()
        await flush()
        gate.resolve('<html><body><p>second</p></body></html>')
        await flush()

        expect(session.pageIndex).toBe(0)
        expect(bodyOf(host).textContent).toContain('first')

        window.dispatchEvent(new Event('pointerdown'))
        scrollTo(10)
        await flush()
        await session.dispose()

        expect(writes().at(-1)![1].status).toBe('reading')
    })

    it('finishes a fragmentless same-document link clicked during settlement', async () => {
        vi.spyOn(HTMLImageElement.prototype, 'complete', 'get').mockReturnValue(false)
        vi.mocked(contentApi.bookStructure).mockResolvedValue({
            spine: [{ href: 'a.xhtml', title: 'A', linear: true, words: 10 }],
            toc: [{ id: 'c1', title: 'One', depth: 0, href: 'a.xhtml', fragment: '' }],
        })
        vi.mocked(contentApi.bookDocument).mockResolvedValue(
            `<html><body><p id="p1">first</p> <p id="p2"><a href="a.xhtml">to the top</a></p> <img src="i.png" /></body></html>`
        )

        const { session, host } = start({ ch: 'a.xhtml' })
        await flush()
        expect(session.restoring).toBe(true)

        const link = bodyOf(host).querySelector('[data-book-href="a.xhtml"]') as HTMLElement
        link.click()
        await flush()

        expect(session.restoring).toBe(false)

        scrollTo(100)
        await flush()
        await session.dispose()
        expect(writes().at(-1)![1].progress!.book).toMatchObject({ anchorId: 'p2' })
    })
})

describe('disposal during a pending mount', () => {
    // No boundary needs a document, so the flow document stays unfetched until
    // the return from standalone asks for it.
    const LATE: BookStructure = {
        spine: [
            { href: 'a.xhtml', title: 'A', linear: true, words: 10 },
            { href: 'notes.xhtml', title: 'Notes', linear: false, words: 5 },
        ],
        toc: [
            { id: 'c1', title: 'One', depth: 0, href: 'a.xhtml', fragment: '' },
            { id: 'n', title: 'Notes', depth: 0, href: 'notes.xhtml', fragment: '' },
        ],
    }

    it('does not leave a navigation waiting for a host that will never arrive', async () => {
        const gate = deferred<string>()
        vi.mocked(contentApi.bookStructure).mockResolvedValue(LATE)
        vi.mocked(contentApi.bookDocument).mockImplementation((_id, href) =>
            href === 'notes.xhtml'
                ? Promise.resolve('<html><body><p id="n">a note</p></body></html>')
                : gate.promise
        )

        const { session } = start({ ch: 'notes.xhtml' })
        await flush()
        expect(session.standalone?.href).toBe('notes.xhtml')

        const returning = session.closeStandalone()
        await flush()

        // The reader unmounts while the destination is still being fetched.
        session.setElements({ host: null, sentinel: null })
        await session.dispose()
        gate.resolve('<html><body><p>back in the flow</p></body></html>')

        const outcome = await Promise.race([
            returning.then(() => 'settled'),
            new Promise(resolve => setTimeout(() => resolve('hung'), 100)),
        ])
        expect(outcome).toBe('settled')
    })
})
