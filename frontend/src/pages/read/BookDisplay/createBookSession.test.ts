import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, reactive, ref } from 'vue'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { contentApi } from '@/utils/api/content'
import { readingApi } from '@/utils/api/reading'
import type {
    BookLocator,
    BookStructure,
    Content,
    ReadingProgress,
    ReadingRequest,
    ReadingResponse,
    ReadingStateResponse,
    ReadingState,
    ReadingStatus,
} from '@/utils/api/types'
import { Modals } from '@/utils/modals'
import { resetReadingActor } from '../readingSync'
import { parseBookEntry, type BookEntry } from './bookEntry'
import { progressPercent } from './bookProgress'
import { zBookSettings, type BookSettings } from './bookSettings'
import { createBookSession, isEmptySlice, type BookSession } from './createBookSession'
import { FakeNav } from './fakeNav'
import { createBookNav } from './useBookDisplayStore'

vi.mock('./bookProgress', async original => {
    const actual = await original<typeof import('./bookProgress')>()
    return { ...actual, progressPercent: vi.fn(actual.progressPercent) }
})
vi.mock('@/utils/api/content', () => ({
    contentApi: {
        get: vi.fn(),
        bookStructure: vi.fn(),
        bookDocument: vi.fn(),
    },
}))

vi.mock('@/utils/modals', async original => ({
    ...(await original<typeof import('@/utils/modals')>()),
    Modals: { show: vi.fn(async () => 'start') },
}))

vi.mock('@/utils/api/reading', () => ({
    readingApi: { get: vi.fn(), post: vi.fn() },
    bumpContinueReading: vi.fn(),
    invalidateReading: vi.fn(),
    markPositionSaved: vi.fn(),
    ReadingConflict: class extends Error {},
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

const doc = (body: string) => `<html><body>${body}</body></html>`

const DOCUMENTS: Record<string, string> = {
    'book.xhtml': doc(`
        <p id="p1">chapter one <a href="book.xhtml#p1b">text</a></p>
        <p id="p1b">more of chapter one</p>
        <p><a href="book.xhtml#legacy">to the old anchor</a></p>
        <p><a href="notes.xhtml#n">footnote</a></p>
        <h2 id="c2">Two</h2>
        <p id="p2">chapter two text</p>
        <h2 id="c3">Three</h2>
        <p id="p3">chapter three text</p>
        <p><a name="legacy">legacy anchor</a></p>`),
    'notes.xhtml': doc(`<p id="n">a note</p>`),
    'extra.xhtml': doc(`<p id="x">off spine</p>`),
}

/** Spine from hrefs (a trailing `!` marks linear="no"); TOC entries are `href` or
 * `href#fragment`, by default one per spine document. */
function book(spine: string[], toc = spine.map(href => href.replace('!', ''))): BookStructure {
    return {
        spine: spine.map(href => ({
            href: href.replace('!', ''),
            title: href,
            linear: !href.endsWith('!'),
            words: 10,
        })),
        toc: toc.map((entry, i) => {
            const [href, fragment = ''] = entry.split('#')
            return { id: `t${i}`, title: entry, depth: 0, href: href!, fragment }
        }),
    }
}

type Served = string | Promise<string> | (() => string | Promise<string>)

/** Unlisted documents 404. */
function serve(structure: BookStructure, docs: Record<string, Served>) {
    vi.mocked(contentApi.bookStructure).mockResolvedValue(structure)
    vi.mocked(contentApi.bookDocument).mockImplementation(async (_id, href) => {
        const found = docs[href]
        if (found === undefined) throw new Error('404')
        return typeof found === 'function' ? found() : found
    })
}

const CONTENT = {
    id: 'c_1',
    file_mtime: '2026-01-01',
    file_size: 1234,
    title: 'A Book',
    type: 'book',
    parent_id: null,
    user_data: null,
} as unknown as Content

let server: ReadingState

/** The saved reading state the session starts from. Writes replace it, as the server does. */
function reading(progress: ReadingProgress = {}, status: ReadingStatus | null = 'reading') {
    server = {
        revision: null,
        status,
        status_updated_at: null,
        progress,
        progress_updated_at: null,
        last_read_at: null,
    }
}

function saved(locator: Omit<BookLocator, 'version'>) {
    reading({ book: { version: 1, ...locator } })
}

/* A fake text layout. Body children are stacked 100px blocks, each slice 1000px
 * below the previous one. Text runs in 20px lines of `charsPerLine` 10px glyphs
 * from each block's top, the glyph box inset inside its line box. Leading and
 * repeated whitespace collapses, `[hidden]` content and zero-width spaces have no
 * box, and `<sub>` glyphs sit `SUB_DROP` lower. */
let scrollY = 0
const BLOCK_HEIGHT = 100
const SLICE_HEIGHT = 1000
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

const box = (top: number, height: number, left = 0, width = 600) =>
    ({
        top,
        bottom: top + height,
        left,
        right: left + width,
        width,
        height,
        x: left,
        y: top,
        toJSON: () => ({}),
    }) as DOMRect
const EMPTY = box(0, 0, 0, 0)

function topOf(el: Element): number {
    const body = el.closest('body')
    if (!body) return 0
    const host = (body.getRootNode() as ShadowRoot).host as HTMLElement | undefined
    const siblings = host?.parentElement ? Array.from(host.parentElement.children) : []
    const base = Math.max(0, siblings.indexOf(host!)) * SLICE_HEIGHT
    const index = Array.from(body.children).findIndex(block => block === el || block.contains(el))
    return base + Math.max(0, index) * BLOCK_HEIGHT
}

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
                const line = Math.floor(visible / charsPerLine)
                const top = topOf(block) - scrollY + line * LINE_HEIGHT + GLYPH_INSET + drop
                return box(top, GLYPH_HEIGHT, (visible % charsPerLine) * 10, 10)
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
    const rects = []
    for (let i = range.startOffset; i < range.endOffset; i++) {
        const rect = glyphRect(range.startContainer as Text, i)
        if (rect.height) rects.push(rect)
    }
    if (!rects.length) return EMPTY
    const top = Math.min(...rects.map(rect => rect.top))
    return box(top, Math.max(...rects.map(rect => rect.bottom)) - top, 0, 100)
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

let settings: BookSettings
const sessions: BookSession[] = []

/** A text setting changed: the session captures the passage before the layout
 * changes (the test's geometry changes after this). */
async function changeText() {
    settings.fontSize += 0.1
    await nextTick()
}

function start(entry: Partial<BookEntry> = {}, nav = new FakeNav(), attach = true) {
    const host = document.createElement('div')
    const sentinel = document.createElement('div')
    document.body.replaceChildren(host, sentinel)
    const session = createBookSession('c_1', { ch: null, frag: null, ...entry }, nav, ref(settings))
    sessions.push(session)
    nav.session = session
    if (attach) session.setElements({ host, sentinel })
    return { session, nav, host, sentinel }
}

/** Past the frame in which the mount scrolled, so later scrolls read as the reader's own. */
async function startTimed(entry: Partial<BookEntry> = {}) {
    vi.useFakeTimers()
    const started = start(entry)
    await vi.advanceTimersByTimeAsync(50)
    await flush()
    return started
}

function bodyOf(host: HTMLElement, index = 0): Element {
    return (host.children[index] as HTMLElement).shadowRoot!.querySelector('body')!
}

function click(host: HTMLElement, selector: string) {
    ;(bodyOf(host).querySelector(selector) as HTMLElement).click()
}

/** A link to a target the document doesn't link to itself. */
function clickNew(host: HTMLElement, href: string, frag = '') {
    const link = document.createElement('a')
    link.setAttribute('data-book-href', href)
    link.setAttribute('data-book-frag', frag)
    bodyOf(host).append(link)
    link.click()
}

/** The book document with an image in its first block. */
const WITH_IMAGE = DOCUMENTS['book.xhtml']!.replace('<p id="p1">', '<p id="p1"><img src="a.png" />')

function respond(req: ReadingRequest): ReadingResponse {
    server = {
        ...server,
        revision: `${req.writer_id}:${req.seq}`,
        progress: 'progress' in req ? req.progress : server.progress,
    }
    return {
        state: server,
        outcome: 'saved',
        previous: null,
        series: null,
        writer: req.writer_id ?? null,
        series_previous: null,
    }
}

/** Position and finish writes, as [contentId, request, init]. */
const writes = () =>
    vi
        .mocked(readingApi.post)
        .mock.calls.filter(([, req]) => req.op === 'position' || req.op === 'finish') as Array<
        [string, ReadingRequest & { progress: ReadingProgress }, RequestInit | undefined]
    >
const lastWrite = () => writes().at(-1)![1]
const completed = () => writes().some(([, req]) => req.op === 'finish')

/** The reader's own scrolling: input on the page arms it, after the frame in which the session's
 * own scrolls count as programmatic. */
async function userScroll(y: number) {
    if (vi.isFakeTimers()) await vi.advanceTimersByTimeAsync(20)
    else await new Promise(resolve => requestAnimationFrame(resolve))
    window.dispatchEvent(new WheelEvent('wheel'))
    scrollTo(y)
}

/** The reader scrolls into the second block; the closing write must carry it. */
async function expectCapturing(session: BookSession) {
    await userScroll(150)
    await flush()
    await session.dispose()
    expect(lastWrite().progress!.book).toMatchObject({ anchorId: 'p1b' })
}

/** Images never finish loading, so a navigation stays settling. */
const stallImages = () =>
    vi.spyOn(HTMLImageElement.prototype, 'complete', 'get').mockReturnValue(false)

const computedStyle = window.getComputedStyle

beforeEach(() => {
    settings = reactive({ ...zBookSettings.parse({}), mode: 'scroll' })
    scrollY = 0
    charsPerLine = CHARS_PER_LINE
    Object.defineProperty(window, 'innerHeight', { value: 768, configurable: true })
    vi.spyOn(window, 'getComputedStyle').mockImplementation((el, pseudo) => {
        const style = computedStyle(el, pseudo)
        return style.writingMode ? style : Object.assign(style, { writingMode: 'horizontal-tb' })
    })
    vi.mocked(contentApi.get).mockResolvedValue(CONTENT)
    reading()
    serve(STRUCTURE, DOCUMENTS)
    vi.mocked(readingApi.get).mockImplementation(async () => ({
        state: server,
        series: null,
        writer: null,
    }))
    vi.mocked(readingApi.post).mockImplementation(async (_id, req) => respond(req))
    window.scrollTo = ((options: { top: number } | number) => {
        scrollY = typeof options === 'number' ? options : options.top
    }) as typeof window.scrollTo
    Object.defineProperty(window, 'scrollY', { get: () => scrollY, configurable: true })
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (
        this: Element
    ) {
        return this.closest('[hidden]') ? EMPTY : box(topOf(this) - scrollY, BLOCK_HEIGHT)
    })
    Object.defineProperty(Range.prototype, 'getBoundingClientRect', {
        value(this: Range) {
            return rangeRect(this)
        },
        configurable: true,
    })
})

afterEach(async () => {
    for (const session of sessions.splice(0)) session.dispose()
    vi.mocked(progressPercent).mockReset()
    resetReadingActor()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    Object.defineProperty(window, 'innerHeight', { value: 768, configurable: true })
    Reflect.deleteProperty(document.documentElement, 'scrollHeight')
})

describe('mounting', () => {
    it('mounts each slice into its own shadow root, keeping html and body', async () => {
        const { session, host } = start()
        await flush()

        expect(session.chapters).toHaveLength(3)
        expect(host.children).toHaveLength(1)
        expect((host.children[0] as HTMLElement).shadowRoot!.querySelector('html > body')).not.toBe(
            null
        )
        expect(bodyOf(host).textContent).toContain('chapter one text')
        expect(bodyOf(host).textContent).not.toContain('chapter two text')
        // Versioned by mtime and size: the response is cached as immutable.
        expect(vi.mocked(contentApi.bookDocument).mock.calls[0]![2]).toBe('2026-01-01-1234')
    })

    it('waits for a host that arrives after the fetches', async () => {
        const { session, host, sentinel } = start({}, new FakeNav(), false)
        await flush()
        expect(host.children).toHaveLength(0)

        session.setElements({ host, sentinel })
        await flush()
        expect(host.children).toHaveLength(1)
    })

    it('re-attaches mounted slices when the host element is replaced', async () => {
        const { session, host } = start()
        await flush()
        const replacement = document.createElement('div')
        session.setElements({ host: replacement, sentinel: null })
        expect(replacement.children).toHaveLength(1)
        expect(host.children).toHaveLength(0)
    })
})

describe('entry resolution', () => {
    it.each([
        ['resumes a saved locator when the URL names no chapter', { textOffset: 60 }, {}, 1, 'c2'],
        [
            'lets a deep link to another chapter win',
            { textOffset: 60 },
            { ch: 'book.xhtml' },
            0,
            '',
        ],
        [
            'recovers a stale locator through its anchor',
            { textOffset: 99_999, anchorId: 'c3' },
            {},
            2,
            'c3',
        ],
    ])('%s', async (_name, locator, entry, chapterIndex, fragment) => {
        saved({ href: 'book.xhtml', ...locator })
        const { session, nav } = start(entry)
        await flush()
        expect(session.chapterIndex).toBe(chapterIndex)
        // The URL is canonicalized onto the chapter it landed on.
        expect(nav.entries[nav.index]!.target).toEqual({ href: 'book.xhtml', fragment })
    })

    it('resolves a stale anchor inside the locator own document of a merged chapter', async () => {
        serve(book(['a.xhtml', 'b.xhtml'], ['a.xhtml']), {
            'a.xhtml': doc('<p id="start">a first</p> <p>a second</p>'),
            'b.xhtml': doc('<p id="start">b first</p> <p>b second</p>'),
        })
        saved({ href: 'b.xhtml', textOffset: 99_999, anchorId: 'start' })
        const { session, host } = start()
        await flush()

        expect(session.chapters).toHaveLength(1)
        expect(host.children).toHaveLength(2)
        // The second slice's band, not the identically-anchored first one.
        expect(scrollY).toBe(SLICE_HEIGHT - 8)
    })

    // Both plates sit at offset 0, so only the anchor tells the chapters apart.
    it.each([
        ['a', 0],
        ['b', 1],
    ])('restores textless plate %s from its anchor', async (anchorId, chapterIndex) => {
        serve(book(['plates.xhtml'], ['plates.xhtml#a', 'plates.xhtml#b']), {
            'plates.xhtml': doc('<img id="a" src="a.png" /> <img id="b" src="b.png" />'),
        })
        saved({ href: 'plates.xhtml', textOffset: 0, anchorId })
        const { session } = start()
        await flush()
        expect(session.chapters).toHaveLength(2)
        expect(session.chapterIndex).toBe(chapterIndex)
    })

    it('navigates to a fragment naming a document body', async () => {
        serve(book(['a.xhtml', 'b.xhtml'], ['a.xhtml', 'b.xhtml#chapter']), {
            'a.xhtml': doc('<p>first</p>'),
            'b.xhtml': '<html><body id="chapter"><p>second</p></body></html>',
        })
        const { session, host } = start({ ch: 'b.xhtml', frag: 'chapter' })
        await flush()

        expect(session.chapters).toHaveLength(2)
        expect(session.chapterIndex).toBe(1)
        expect(session.notice).toBeNull()
        expect(bodyOf(host).textContent).toContain('second')
    })
})

describe('navigation races', () => {
    const THREE = book(['a.xhtml', 'b.xhtml', 'c.xhtml'])
    const text = (href: string) => doc(`<p id="${href}">in ${href}</p>`)

    it('keeps the last requested chapter when an earlier fetch resolves late', async () => {
        const b = deferred<string>()
        const c = deferred<string>()
        serve(THREE, { 'a.xhtml': text('a.xhtml'), 'b.xhtml': b.promise, 'c.xhtml': c.promise })
        const { session, host, nav } = start()
        await flush()
        expect(session.chapterIndex).toBe(0)

        session.goToChapter(1)
        await flush()
        // As Contents does: turns are dropped while a chapter is pending.
        nav.push({ href: 'c.xhtml', fragment: '' })
        await flush()

        // Both destinations are still in flight; the abandoned one lands first.
        c.resolve(text('c.xhtml'))
        await flush()
        b.resolve(text('b.xhtml'))
        await flush()

        expect(session.chapterIndex).toBe(2)
        expect(bodyOf(host).textContent).toContain('in c.xhtml')
        expect(bodyOf(host).textContent).not.toContain('in b.xhtml')
        expect(nav.entries[nav.index]!.target.href).toBe('c.xhtml')
    })

    it('drops repeated turns while a crossing is pending, then crosses again once it commits', async () => {
        const gate = deferred<string>()
        serve(THREE, {
            'a.xhtml': text('a.xhtml'),
            'b.xhtml': gate.promise,
            'c.xhtml': text('c.xhtml'),
        })
        const { session, host, nav } = start()
        await flush()
        const pushes = nav.entries.length

        session.goToChapter(1)
        await flush()
        // Held `.`: the chapter index is still the old one, so it asks for B again.
        session.goToChapter(session.chapterIndex + 1)
        session.goToChapter(session.chapterIndex + 1)
        expect(nav.entries.length).toBe(pushes + 1)

        gate.resolve(text('b.xhtml'))
        await flush()
        expect(session.chapterIndex).toBe(1)
        expect(bodyOf(host).textContent).toContain('in b.xhtml')
        expect(session.restoring).toBe(false)
        scrollTo(0)
        session.snapshotPassage()
        expect(nav.historyLocator()).toMatchObject({ href: 'b.xhtml' })

        session.goToChapter(session.chapterIndex + 1)
        await flush()
        expect(session.chapterIndex).toBe(2)
    })

    it('prefetches the next chapter when idle, so crossing fetches nothing', async () => {
        const idle: Array<() => void> = []
        vi.stubGlobal('requestIdleCallback', (job: () => void) => idle.push(job))
        vi.stubGlobal('cancelIdleCallback', () => {})
        serve(THREE, Object.fromEntries(['a', 'b', 'c'].map(id => [`${id}.xhtml`, text(id)])))
        const { session } = start({ ch: 'b.xhtml' })
        await flush()
        const fetched = () => vi.mocked(contentApi.bookDocument).mock.calls.map(call => call[1])
        expect(fetched()).toEqual(['b.xhtml'])

        idle.shift()!()
        await flush()
        expect(fetched()).toEqual(['b.xhtml', 'c.xhtml', 'a.xhtml'])

        session.goToChapter(2)
        await flush()
        expect(session.chapterIndex).toBe(2)
        expect(fetched()).toHaveLength(3)
    })

    it('clears the crossing when the router refuses the push', async () => {
        serve(THREE, Object.fromEntries(['a', 'b', 'c'].map(id => [`${id}.xhtml`, text(id)])))
        const router = createRouter({
            history: createMemoryHistory(),
            routes: [{ path: '/r/:id', component: { template: '<div />' } }],
        })
        await router.push('/r/c_1')
        let refuse = false
        router.beforeEach(() => !refuse)
        const host = document.createElement('div')
        document.body.replaceChildren(host)
        const nav = createBookNav(router, 'c_1')
        const session = createBookSession('c_1', { ch: null, frag: null }, nav, ref(settings))
        sessions.push(session)
        router.afterEach((to, _from, failure) => {
            if (!failure) session.setEntry(parseBookEntry(to.query))
        })
        session.setElements({ host, sentinel: null })
        await flush()
        await router.isReady()

        refuse = true
        session.goToChapter(1)
        await flush()
        expect(session.chapterIndex).toBe(0)

        refuse = false
        session.goToChapter(1)
        await flush(60)
        expect(session.chapterIndex).toBe(1)
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
        expect(session.chapters).toHaveLength(3)
        expect(session.chapterIndex).toBe(2)
    })

    it('finishes restoration when a mounted link is followed mid-settlement', async () => {
        stallImages()
        serve(STRUCTURE, {
            ...DOCUMENTS,
            'book.xhtml': WITH_IMAGE,
        })
        const { session, host } = start()
        await flush()
        expect(session.restoring).toBe(true)

        click(host, '[data-book-frag="p1b"]')
        await flush()
        expect(session.restoring).toBe(false)
        expect(scrollY).toBe(BLOCK_HEIGHT - 8)
    })

    it('finishes a fragmentless same-document link clicked during settlement', async () => {
        stallImages()
        serve(book(['a.xhtml']), {
            'a.xhtml': doc(
                `<p id="p1">first</p> <p id="p2"><a href="a.xhtml">to the top</a></p> <img src="i.png" />`
            ),
        })
        const { session, host } = start({ ch: 'a.xhtml' })
        await flush()
        expect(session.restoring).toBe(true)

        click(host, '[data-book-href="a.xhtml"]')
        await flush()
        expect(session.restoring).toBe(false)

        await userScroll(100)
        await flush()
        await session.dispose()
        expect(lastWrite().progress!.book).toMatchObject({ anchorId: 'p2' })
    })

    it('does not let a disposed session rewrite the route', async () => {
        const { session, nav } = start()
        const seen = nav.entries.length
        void session.dispose()
        await flush()
        expect(nav.entries.length).toBe(seen)
    })
})

describe('failed navigations', () => {
    const WITH_GONE = book(['book.xhtml', 'gone.xhtml'])

    it('keeps capturing on the chapter it kept after Next fails', async () => {
        serve(WITH_GONE, DOCUMENTS)
        const { session } = start()
        await flush()

        session.goToChapter(1)
        await flush()
        expect(session.chapterIndex).toBe(0)
        expect(session.error).toBeNull()

        await expectCapturing(session)
    })

    it('keeps the chapter, and hands it back, when a link fails mid-settlement', async () => {
        stallImages()
        serve(WITH_GONE, {
            'book.xhtml': WITH_IMAGE,
        })
        const { session, host } = start()
        await flush()
        expect(session.restoring).toBe(true)

        clickNew(host, 'gone.xhtml')
        await flush()
        expect(session.restoring).toBe(false)
        expect(session.error).toBeNull()
        expect(session.notice).toContain('missing')
        expect(session.chapterIndex).toBe(0)
        expect(bodyOf(host).textContent).toContain('chapter one')

        await expectCapturing(session)
    })

    it('keeps capturing after an unreachable off-spine Contents selection', async () => {
        const { session } = start()
        await flush()

        // A Contents link to a document no longer in the archive, once the router has moved.
        session.setEntry({ ch: 'nowhere.xhtml', frag: null })
        await flush()
        expect(session.standalone).toBeNull()
        expect(session.notice).toContain('unavailable')
        expect(session.restoring).toBe(false)
        await expectCapturing(session)
    })

    it('ignores a stale standalone failure while a newer navigation settles', async () => {
        stallImages()
        const gate = deferred<string>()
        serve(STRUCTURE, {
            // Only the later chapter carries the image that blocks settling.
            'book.xhtml': DOCUMENTS['book.xhtml']!.replace('<p id="p2">', '<img /><p id="p2">'),
            'notes.xhtml': gate.promise,
        })
        const { session, host, nav } = start()
        await flush()
        expect(session.restoring).toBe(false)

        click(host, '[data-book-href="notes.xhtml"]')
        await flush()
        session.goToChapter(1)
        await flush()
        expect(session.standalone).toBeNull()
        expect(session.restoring).toBe(true)

        const stamped = JSON.stringify(nav.entries)
        gate.reject(new Error('too late'))
        await flush()
        expect(session.restoring).toBe(true)
        expect(JSON.stringify(nav.entries)).toBe(stamped)
    })

    it('does not complete the book after a cancelled move to the final chapter', async () => {
        const gate = deferred<string>()
        serve(book(['a.xhtml', 'b.xhtml']), {
            'a.xhtml': doc(`<p id="p1">first</p> <p><a href="nowhere.xhtml">broken</a></p>`),
            'b.xhtml': gate.promise,
        })
        const { session, host } = start()
        await flush()

        session.goToChapter(1)
        await flush()
        // The final chapter never mounts: a link still on screen cancels it, then fails itself.
        click(host, '[data-book-href="nowhere.xhtml"]')
        await flush()
        gate.resolve(doc('<p>second</p>'))
        await flush()
        expect(session.chapterIndex).toBe(0)
        expect(bodyOf(host).textContent).toContain('first')

        await userScroll(10)
        await flush()
        await session.dispose()
        expect(writes().length).toBeGreaterThan(0)
        expect(completed()).toBe(false)
    })

    it('fetches a document again after a failed attempt, clearing the notice', async () => {
        let failures = 1
        serve(book(['a.xhtml', 'b.xhtml']), {
            'a.xhtml': doc('<p>in a.xhtml</p>'),
            'b.xhtml': () =>
                failures-- > 0 ? Promise.reject(new Error('503')) : doc('<p>in b.xhtml</p>'),
        })
        const { session, host } = start()
        await flush()

        session.goToChapter(1)
        await flush()
        expect(session.chapterIndex).toBe(0)
        expect(session.notice).toBeTruthy()

        session.goToChapter(1)
        await flush()
        expect(session.chapterIndex).toBe(1)
        expect(bodyOf(host).textContent).toContain('in b.xhtml')
        expect(session.notice).toBeNull()
    })

    it('puts the route back on the chapter shown, so a fragment target can be retried', async () => {
        // The second chapter opens mid-b and runs through c, which fails once.
        let failures = 1
        const text = (id: string) => doc(`<p id="${id}1">in ${id}.xhtml</p>`)
        serve(
            book(
                ['a.xhtml', 'b.xhtml', 'c.xhtml', 'd.xhtml'],
                ['a.xhtml', 'b.xhtml#b1', 'd.xhtml']
            ),
            {
                'a.xhtml': text('a'),
                'b.xhtml': text('b'),
                'c.xhtml': () => (failures-- > 0 ? Promise.reject(new Error('503')) : text('c')),
                'd.xhtml': text('d'),
            }
        )
        const { session, host, nav } = start()
        await flush()

        session.goToChapter(1)
        await flush()
        expect(session.chapterIndex).toBe(0)
        expect(session.notice).toBeTruthy()
        expect(nav.entries[nav.index]!.target).toEqual({ href: 'a.xhtml', fragment: '' })

        session.goToChapter(1)
        await flush()
        expect(session.chapterIndex).toBe(1)
        expect(bodyOf(host, 1).textContent).toContain('in c.xhtml')
    })
})

describe('progress', () => {
    it('counts a scroll not yet captured as reading when a check finds another device', async () => {
        const { session } = await startTimed()
        await userScroll(150)
        server = {
            ...server,
            revision: 'srv:other',
            progress: {
                book: { version: 1, href: 'book.xhtml', textOffset: 5 },
                progress_percent: 2,
            },
        }
        session.sync.check()
        await flush()
        expect(Modals.show).toHaveBeenCalledWith(
            expect.anything(),
            expect.objectContaining({ kind: 'moved' })
        )
    })

    it('counts reading made while a check is out', async () => {
        const { session } = await startTimed()
        const read = Promise.withResolvers<ReadingStateResponse>()
        vi.mocked(readingApi.get).mockReturnValueOnce(read.promise)
        session.sync.check()
        await userScroll(150)
        read.resolve({
            state: {
                ...server,
                revision: 'srv:other',
                progress: {
                    book: { version: 1, href: 'book.xhtml', textOffset: 5 },
                    progress_percent: 2,
                },
            },
            series: null,
            writer: null,
        })
        await flush()
        expect(Modals.show).toHaveBeenCalledWith(
            expect.anything(),
            expect.objectContaining({ kind: 'moved' })
        )
    })

    it('lets Mark completed replace a scroll not yet captured', async () => {
        const { session } = await startTimed()
        await userScroll(150)
        await session.sync.command({ op: 'mark_completed' })
        await vi.advanceTimersByTimeAsync(1500)
        expect(vi.mocked(readingApi.post).mock.calls.map(([, req]) => req.op)).toEqual([
            'mark_completed',
        ])
    })

    it('writes the passage the reader scrolled to as a fresh position', async () => {
        reading({ current_page: 7 }, null)
        const { session } = await startTimed()
        await userScroll(150)
        await flush()
        await session.leave()
        await session.dispose()

        expect(writes()).toHaveLength(1)
        const [, req] = writes()[0]!
        expect(req.op).toBe('position')
        expect(req.progress.current_page).toBeUndefined()
        expect(req.progress.book).toMatchObject({ version: 1, href: 'book.xhtml', anchorId: 'p1b' })
        expect(req.progress.book!.textOffset).toBeGreaterThan(0)
        // Partway through the only linear document.
        expect(req.progress.progress_percent).toBeGreaterThan(0)
        expect(req.progress.progress_percent).toBeLessThan(100)
        expect(req).toMatchObject({ base_revision: null, seq: expect.any(Number) })
    })

    it('flushes with keepalive when the tab is hidden, and resumes afterwards', async () => {
        const { session } = await startTimed()
        const visibility = (value: string) => {
            Object.defineProperty(document, 'visibilityState', { value, configurable: true })
            document.dispatchEvent(new Event('visibilitychange'))
        }
        visibility('hidden')
        await flush()
        expect(writes()).toHaveLength(0)

        await userScroll(150)
        visibility('hidden')
        await flush()
        expect(writes()).toHaveLength(1)
        expect(writes()[0]![2]).toEqual({ keepalive: true })

        visibility('visible')
        await flush()
        await userScroll(250)
        await flush()
        await session.dispose()
        expect(writes()).toHaveLength(2)
        expect(writes()[1]![1].seq).toBeGreaterThan(writes()[0]![1].seq!)
    })

    it.each(['dropped', null] as const)('writes nothing on open and close (%s)', async status => {
        reading({ book: { version: 1, href: 'book.xhtml', textOffset: 60 } }, status)
        const { session } = await startTimed()
        await session.dispose()
        expect(writes()).toHaveLength(0)
    })

    it('reopens a finished book at its end, unless the URL names a passage', async () => {
        reading({ book: { version: 1, href: 'book.xhtml', textOffset: 5 }, at_end: true })
        const finished = start().session
        await flush()
        expect(finished.chapterIndex).toBe(2)
        const linked = start({ ch: 'book.xhtml', frag: 'c2' }).session
        await flush()
        expect(linked.chapterIndex).toBe(1)
    })

    it.each([
        ['a chapter', {}],
        ['a passage in it', { frag: 'mid' }],
    ])('lets a URL naming %s of a finished book beat its end', async (_name, entry) => {
        serve(book(['a.xhtml', 'b.xhtml']), {
            'a.xhtml': doc('<p>a</p>'),
            'b.xhtml': doc('<p>b first</p> <p id="mid">b middle</p> <p id="late">b late</p>'),
        })
        const landing = async (progress: ReadingProgress) => {
            reading(progress)
            start({ ch: 'b.xhtml', ...entry })
            await flush()
            return scrollY
        }
        const plain = await landing({})
        const finished = await landing({
            book: { version: 1, href: 'b.xhtml', textOffset: 99_999, anchorId: 'late' },
            at_end: true,
        })
        expect(finished).toBe(plain)
        expect(
            await landing({
                book: { version: 1, href: 'b.xhtml', textOffset: 99_999, anchorId: 'late' },
            })
        ).not.toBe(plain)
    })

    it('saves where a reading crossing lands, and nothing for a placement', async () => {
        const { session } = await startTimed()
        session.goToChapter(1)
        await vi.advanceTimersByTimeAsync(300)
        expect(session.chapterIndex).toBe(1)
        session.goToChapter(2, { moved: true })
        await vi.advanceTimersByTimeAsync(1500)
        expect(session.chapterIndex).toBe(2)
        expect(writes()).toHaveLength(1)
        expect(lastWrite().progress.book).toMatchObject({ href: 'book.xhtml', anchorId: 'p3' })

        // Back a chapter, as the on-page Previous chapter goes.
        session.goToChapter(1, { moved: true })
        await vi.advanceTimersByTimeAsync(1500)
        expect(session.chapterIndex).toBe(1)
        expect(writes()).toHaveLength(2)
    })

    it('writes nothing for scrolling after input in a drawer', async () => {
        const { session } = await startTimed()
        const drawer = document.createElement('div')
        drawer.setAttribute('role', 'dialog')
        document.body.append(drawer)
        drawer.dispatchEvent(new Event('pointerdown', { bubbles: true }))
        scrollTo(150)
        await vi.advanceTimersByTimeAsync(1500)
        await session.dispose()
        expect(writes()).toHaveLength(0)
    })
})

describe('completion', () => {
    it('finishes when an armed scroll brings the end into view', async () => {
        const { session } = start()
        await flush()
        session.goToChapter(2)
        await flush()
        expect(session.chapterIndex).toBe(2)

        scrollTo(10)
        await flush()
        expect(completed()).toBe(false)
        // Input with the end already in view is no finish.
        window.dispatchEvent(new Event('pointerdown'))
        await flush()
        expect(completed()).toBe(false)

        await userScroll(5000)
        await userScroll(10)
        await flush()
        await session.dispose()
        expect(lastWrite()).toMatchObject({
            op: 'finish',
            progress: { progress_percent: 100, at_end: true, book: { href: 'book.xhtml' } },
        })
    })

    it('saves a turn back from the end, even within its last percent', async () => {
        const { session } = start()
        await flush()
        session.goToChapter(2)
        await flush()
        scrollTo(10)
        window.dispatchEvent(new Event('pointerdown'))
        await userScroll(5000)
        await userScroll(10)
        await flush()
        expect(lastWrite()).toMatchObject({ op: 'finish' })

        vi.mocked(progressPercent).mockReturnValue(99.2)
        await userScroll(200)
        await flush()
        await session.dispose()
        expect(lastWrite()).toMatchObject({
            op: 'position',
            progress: { progress_percent: 99.2, book: { textOffset: 112 } },
        })
    })

    it('finishes again after accepting a clear made elsewhere', async () => {
        const { session } = start()
        await flush()
        const finishAtEnd = async () => {
            session.goToChapter(2)
            await flush()
            scrollTo(10)
            window.dispatchEvent(new Event('pointerdown'))
            await userScroll(5000)
            await userScroll(10)
            await flush()
        }
        await finishAtEnd()
        server = { ...server, revision: 'srv:clear', status: null, progress: {} }
        session.sync.check()
        await flush()
        await finishAtEnd()
        await session.dispose()
        expect(writes().filter(([, req]) => req.op === 'finish')).toHaveLength(2)
    })

    it('follows another device to the end of a finished book, not to its locator', async () => {
        reading({ book: { version: 1, href: 'book.xhtml', textOffset: 5 } }, 'completed')
        const { session } = start()
        await flush()
        expect(session.chapterIndex).toBe(0)
        // Finished elsewhere, under a layout whose finishing locator sits early in the book.
        server = {
            ...server,
            revision: 'srv:other',
            progress: {
                book: { version: 1, href: 'book.xhtml', textOffset: 60 },
                progress_percent: 100,
                at_end: true,
            },
        }
        session.sync.check()
        await flush()
        expect(session.chapterIndex).toBe(2)
        await session.dispose()
    })

    it('tells a late locator in a chapter sharing the last file from the end', async () => {
        reading({ progress_percent: 100, at_end: true }, 'completed')
        const { session } = start()
        await flush()
        expect(session.chapterIndex).toBe(2)
        server = {
            ...server,
            revision: 'srv:other',
            progress: {
                book: { version: 1, href: 'book.xhtml', textOffset: 60 },
                progress_percent: 99.5,
            },
        }
        session.sync.check()
        await flush()
        expect(session.chapterIndex).toBe(1)
        await session.dispose()
    })

    it('finishes when key paging brings the end into view on its first scroll', async () => {
        const { session } = start()
        await flush()
        session.goToChapter(2)
        await flush()
        scrollTo(5000)
        await flush()
        await new Promise(resolve => requestAnimationFrame(resolve))
        session.arm()
        scrollTo(10)
        await flush()
        await session.dispose()
        expect(completed()).toBe(true)
    })

    it('opens again after its saved state failed to load', async () => {
        reading({ progress_percent: 100, at_end: true }, 'completed')
        vi.mocked(readingApi.get).mockRejectedValueOnce(new Error('down'))
        const { session } = start()
        await flush()
        expect(session).toMatchObject({ error: 'down', openFailed: true, loading: false })
        session.retry()
        await flush()
        expect(session).toMatchObject({ error: null, openFailed: false, loading: false })
        expect(session.chapterIndex).toBe(2)
        await session.dispose()
    })

    it('ignores crossings and turns before the saved position is placed', async () => {
        reading({ progress_percent: 100, at_end: true }, 'completed')
        const served = vi.mocked(readingApi.get).getMockImplementation()!
        const gate = deferred<void>()
        vi.mocked(readingApi.get).mockImplementationOnce(async id => {
            await gate.promise
            return served(id)
        })
        const { session } = start()
        await flush()
        expect(session.loading).toBe(true)
        session.goToChapter(1, { moved: true })
        session.turn('next')
        gate.resolve()
        await flush()
        expect(session.chapterIndex).toBe(2)
        await session.dispose()
        expect(writes()).toHaveLength(0)
    })

    // Reading armed with the end out of view; a link in the last chapter then shows it.
    it('does not finish for a link placed within the chapter', async () => {
        const { session, host } = start()
        await flush()
        session.goToChapter(2)
        await flush()
        await userScroll(5000)
        await flush()
        clickNew(host, 'book.xhtml', 'legacy')
        await flush()
        await userScroll(scrollY - 10)
        await flush()
        await session.dispose()
        expect(completed()).toBe(false)
    })

    it('does not finish for a link to the entry shown', async () => {
        serve(book(['a.xhtml', 'b.xhtml']), {
            'a.xhtml': doc('<p id="a1">first</p>'),
            'b.xhtml': doc('<p id="b1">last</p>'),
        })
        const { session, host } = start({ ch: 'b.xhtml' })
        await flush()
        expect(session.chapterIndex).toBe(1)
        await userScroll(5000)
        await flush()
        clickNew(host, 'b.xhtml', 'vanished')
        await flush()
        await userScroll(scrollY + 3)
        await flush()
        await session.dispose()
        expect(completed()).toBe(false)
    })

    it('takes no reading or finish from scroll anchoring after a resize', async () => {
        const { session } = start()
        await flush()
        session.goToChapter(2)
        await flush()
        await userScroll(4000)
        await userScroll(5000)
        await flush()
        window.dispatchEvent(new Event('resize'))
        await flush()
        const before = writes().length
        // Native anchoring brings the end into view, with no input.
        scrollTo(10)
        await flush()
        await session.dispose()
        expect(writes()).toHaveLength(before)
    })

    it('does not complete when the sentinel is above the viewport', async () => {
        const { session } = start()
        await flush()
        session.goToChapter(2)
        await flush()
        scrollTo(5000)
        window.dispatchEvent(new Event('pointerdown'))
        await flush()
        await session.dispose()
        expect(completed()).toBe(false)
    })

    // On the last chapter, where everything but the standalone guard says complete.
    it('never completes a book from standalone viewing', async () => {
        const { session, host } = start()
        await flush()
        session.goToChapter(2)
        await flush()
        await userScroll(10)
        await flush()

        clickNew(host, 'notes.xhtml')
        await flush()
        expect(session.standalone).not.toBeNull()

        window.dispatchEvent(new Event('pointerdown'))
        await flush()
        await session.dispose()
        expect(writes().length).toBeGreaterThan(0)
        expect(completed()).toBe(false)
    })
})

describe('standalone documents', () => {
    it('returns to the passage it was opened from', async () => {
        const { session, host } = start()
        await flush()
        scrollTo(150)
        await flush()

        click(host, '[data-book-href="notes.xhtml"]')
        await flush()
        expect(session.standalone).toMatchObject({ href: 'notes.xhtml', title: 'Notes' })
        expect(bodyOf(host).textContent).toContain('a note')

        session.closeStandalone()
        await flush()
        expect(session.standalone).toBeNull()
        expect(session.chapterIndex).toBe(0)
        expect(scrollY).toBe(restoredAt(2 * BLOCK_HEIGHT))
    })

    it('puts the reading route back when one opened from the URL is closed', async () => {
        const { session, nav } = start({ ch: 'notes.xhtml' })
        await flush()
        expect(session.standalone?.href).toBe('notes.xhtml')

        await session.closeStandalone()
        await flush()
        expect(session.standalone).toBeNull()
        expect(nav.entries[nav.index]!.target.href).toBe('book.xhtml')
    })

    it('never writes a standalone document into progress', async () => {
        const { session, host } = start()
        await flush()
        await userScroll(150)
        await flush()
        expect(writes()).toHaveLength(0)

        click(host, '[data-book-href="notes.xhtml"]')
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
        expect(writes().every(([, req]) => req.progress.book!.href === 'book.xhtml')).toBe(true)
    })

    it('opens an off-spine document and reports one that is missing', async () => {
        const { session } = start({ ch: 'extra.xhtml' })
        await flush()
        expect(session.standalone?.href).toBe('extra.xhtml')

        const gone = start({ ch: 'nowhere.xhtml' })
        await flush()
        expect(gone.session.standalone).toBeNull()
        expect(gone.session.notice).toContain('unavailable')
        expect(gone.session.chapterIndex).toBe(0)
    })
})

describe('internal links', () => {
    it('follows a legacy name anchor onto its own chapter', async () => {
        const { session, host } = start()
        await flush()
        click(host, '[data-book-frag="legacy"]')
        await flush()

        expect(session.chapterIndex).toBe(2)
        expect(scrollY).toBe(2 * BLOCK_HEIGHT - 8)
        expect(session.notice).toBeNull()
    })

    it('reports a link whose fragment no longer exists', async () => {
        const { session, host } = start()
        await flush()
        clickNew(host, 'book.xhtml', 'vanished')
        await flush()
        expect(session.notice).toContain('unavailable')
    })
})

describe('history', () => {
    it('returns to the passage left behind on Back, and to the new one on Forward', async () => {
        const { session, nav } = await startTimed()
        scrollTo(150)
        await vi.advanceTimersByTimeAsync(600)
        expect(nav.entries[0]!.locator?.anchorId).toBe('p1b')

        session.goToChapter(2)
        await vi.advanceTimersByTimeAsync(300)
        expect(session.chapterIndex).toBe(2)

        // Read on at the destination, then leave and come back both ways.
        await vi.advanceTimersByTimeAsync(50)
        scrollTo(restoredAt(BLOCK_HEIGHT))
        await vi.advanceTimersByTimeAsync(600)
        expect(nav.entries[1]!.locator?.anchorId).toBe('p3')

        nav.go(-1)
        await vi.advanceTimersByTimeAsync(300)
        expect(session.chapterIndex).toBe(0)
        expect(scrollY).toBe(restoredAt(2 * BLOCK_HEIGHT))

        nav.go(1)
        await vi.advanceTimersByTimeAsync(300)
        expect(session.chapterIndex).toBe(2)
        expect(scrollY).toBe(restoredAt(BLOCK_HEIGHT))
    })

    it('lands at the bottom of a chapter reached going back, once', async () => {
        const { session } = start()
        await flush()
        session.goToChapter(1)
        await flush()
        expect(session.chapterIndex).toBe(1)

        Object.defineProperty(document.documentElement, 'scrollHeight', {
            value: 4000,
            configurable: true,
        })
        session.goToChapter(0, { atEnd: true })
        await flush()
        expect(session.chapterIndex).toBe(0)
        expect(scrollY).toBe(4000)

        // The landing is spent, so the next arrival is positioned normally.
        session.goToChapter(1)
        await flush()
        expect(scrollY).toBe(0)
    })

    it('drops the landing when the turn is overtaken by another entry', async () => {
        const { session } = start()
        await flush()
        session.goToChapter(1)
        await flush()

        Object.defineProperty(document.documentElement, 'scrollHeight', {
            value: 4000,
            configurable: true,
        })
        session.goToChapter(0, { atEnd: true })
        // Same chapter, but a deliberate anchor: it must not inherit the landing.
        session.setEntry({ ch: 'book.xhtml', frag: 'p1b' })
        await flush()
        expect(session.chapterIndex).toBe(0)
        expect(scrollY).toBe(BLOCK_HEIGHT - 8)
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

    it('runs the leave hook only while the entry is still the one being left', async () => {
        const real = createRouter({
            history: createMemoryHistory(),
            routes: [{ path: '/r/:id', component: { template: '<div />' } }],
        })
        await real.push('/r/c_1')
        const leave = vi.fn()
        const remove = createBookNav(real, 'c_1').beforeLeave(leave)
        history.replaceState({ current: '/r/c_1' }, '')
        await real.push('/r/c_1?ch=b.xhtml')
        expect(leave).toHaveBeenCalledOnce()
        // As after Back: the browser entry has already moved.
        history.replaceState({ current: '/r/c_1?ch=c.xhtml' }, '')
        await real.push('/r/c_1?ch=d.xhtml')
        expect(leave).toHaveBeenCalledOnce()
        remove()
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
    serve(book(['long.xhtml']), { 'long.xhtml': doc(body) })
    if (locator) saved({ href: 'long.xhtml', ...locator })
}

describe('reflow', () => {
    it('saves reading still waiting for its capture before a layout change', async () => {
        const { session } = await startTimed()
        await userScroll(150)
        await vi.advanceTimersByTimeAsync(100) // inside the capture debounce
        await changeText()
        await vi.advanceTimersByTimeAsync(1500)
        expect(lastWrite().op).toBe('position')
        expect(lastWrite().progress!.book).toMatchObject({ anchorId: 'p1b' })
        await session.dispose()
    })

    it('restores the passage captured before the layout change', async () => {
        const { session } = await startTimed()
        scrollTo(150)
        await vi.advanceTimersByTimeAsync(300)
        const before = session.percent

        await changeText()
        scrollY = 0
        await vi.advanceTimersByTimeAsync(300)
        expect(scrollY).toBe(restoredAt(2 * BLOCK_HEIGHT))
        expect(session.percent).toBe(before)
    })

    it('leaves window resizes to native scroll anchoring', async () => {
        await startTimed()
        scrollTo(150)
        await vi.advanceTimersByTimeAsync(300)
        window.dispatchEvent(new Event('resize'))
        await vi.advanceTimersByTimeAsync(300)
        expect(scrollY).toBe(150)
    })

    const SUB_LINE = 'abcd<sub>e</sub>fghi '.repeat(5)
    it.each([
        [
            'recaptures a mid-paragraph line where it was restored',
            LONG_BODY,
            restoredAt(BLOCK_HEIGHT, 2) - 5,
            restoredAt(BLOCK_HEIGHT, 2),
        ],
        [
            'does not drift into a block whose last line straddles the top',
            LONG_BODY,
            2 * BLOCK_HEIGHT - 10,
            restoredAt(2 * BLOCK_HEIGHT),
        ],
        [
            'does not drift onto lower glyphs of the line above',
            `<p>${SUB_LINE}</p> <p>${SUB_LINE}</p>`,
            restoredAt(BLOCK_HEIGHT, 2),
            restoredAt(BLOCK_HEIGHT, 2),
        ],
    ])('%s', async (_name, body, from, expected) => {
        withBook(body)
        await startTimed()
        scrollTo(from)
        await vi.advanceTimersByTimeAsync(300)
        // Each round rescrolls in place, so the next reflow captures afresh
        // instead of reusing its anchor.
        for (let i = 0; i < 3; i++) {
            await changeText()
            await vi.advanceTimersByTimeAsync(300)
            expect(scrollY).toBe(expected)
            scrollTo(scrollY)
            await vi.advanceTimersByTimeAsync(300)
        }
    })

    it('reuses its anchor, now mid-line, across layout changes until the reader scrolls', async () => {
        withBook(LONG_BODY)
        const { nav } = await startTimed()
        scrollTo(restoredAt(BLOCK_HEIGHT, 2) - 5)
        await vi.advanceTimersByTimeAsync(600)
        const anchor = 50 + 2 * CHARS_PER_LINE
        expect(nav.historyLocator()?.textOffset).toBe(anchor)

        await changeText()
        charsPerLine = 7
        scrollTo(scrollY - 3)
        await vi.advanceTimersByTimeAsync(600)
        expect(scrollY).toBe(restoredAt(BLOCK_HEIGHT, 2))
        expect(nav.historyLocator()?.textOffset).toBe(anchor)

        await changeText()
        charsPerLine = 6
        await vi.advanceTimersByTimeAsync(600)
        expect(scrollY).toBe(restoredAt(BLOCK_HEIGHT, 3))
        expect(nav.historyLocator()?.textOffset).toBe(anchor)

        scrollTo(scrollY + 1)
        await vi.advanceTimersByTimeAsync(600)
        expect(nav.historyLocator()?.textOffset).toBe(50 + 3 * 6)
    })

    describe('anchor lifetime', () => {
        const P2 = 'chapter one textmore of chapter oneto the old anchorfootnoteTwo'.length
        const THIRD_BLOCK = 'chapter one text'.length + 'more of chapter one'.length

        async function reflowed() {
            const started = await startTimed()
            scrollTo(150)
            await vi.advanceTimersByTimeAsync(600)
            await changeText()
            await vi.advanceTimersByTimeAsync(600)
            expect(started.nav.historyLocator()?.textOffset).toBe(THIRD_BLOCK)
            return started
        }

        it('ends at a same-page link', async () => {
            const { host, nav } = await reflowed()
            click(host, '[data-book-frag="p1b"]')
            await vi.advanceTimersByTimeAsync(600)
            expect(scrollY).toBe(BLOCK_HEIGHT - 8)
            expect(nav.historyLocator()).toMatchObject({
                textOffset: 'chapter one text'.length,
                anchorId: 'p1b',
            })
        })

        it('ends at a navigation', async () => {
            const { session, nav } = await reflowed()
            session.goToChapter(1)
            await vi.advanceTimersByTimeAsync(600)
            expect(session.chapterIndex).toBe(1)
            expect(nav.historyLocator()).toMatchObject({ textOffset: P2, anchorId: 'p2' })

            nav.go(-1)
            await vi.advanceTimersByTimeAsync(600)
            nav.go(1)
            await vi.advanceTimersByTimeAsync(600)
            expect(session.chapterIndex).toBe(1)
            expect(scrollY).toBe(restoredAt(BLOCK_HEIGHT))
        })
    })
})

describe('character positions', () => {
    const SPACED = `<p>${PARA}</p> <p>abcdefghij klmnopqrs</p> <p>abcdefghijklmno\u200bpqrstuvwxyzabcd</p>`
    // Offsets count collapsed text: LONG_BODY paragraphs are 50 characters, plus one
    // collapsed space for the indentation opening the third.
    const LINE_2 = 50 + 2 * CHARS_PER_LINE
    it.each<{ name: string; body: string; y: number; at: object; innerHeight?: number }>([
        {
            name: 'the first visible line, not the start of its block',
            body: LONG_BODY,
            y: restoredAt(BLOCK_HEIGHT, 2) - 5,
            at: { textOffset: LINE_2, anchorId: 'l1' },
        },
        {
            name: 'the straddling line when no glyph top is in view',
            body: LONG_BODY,
            y: restoredAt(BLOCK_HEIGHT, 2) + 5,
            at: { textOffset: LINE_2, anchorId: 'l1' },
            innerHeight: 20,
        },
        {
            name: 'the start of a textless block filling the view',
            body: `<p>${PARA}</p> <div id="blank"></div> <p>${PARA}</p>`,
            y: BLOCK_HEIGHT,
            at: { textOffset: 50, anchorId: 'blank' },
            innerHeight: 20,
        },
        {
            name: 'past collapsed indentation',
            body: LONG_BODY,
            y: restoredAt(2 * BLOCK_HEIGHT),
            at: { textOffset: 2 * 50 + 1 },
        },
        {
            name: 'past hidden text',
            body: LONG_BODY,
            y: restoredAt(3 * BLOCK_HEIGHT),
            at: { textOffset: 3 * 50 + 1 + 'PAGEBREAK'.length },
        },
        {
            name: 'past a laid-out space opening a line',
            body: SPACED,
            y: restoredAt(BLOCK_HEIGHT, 1),
            at: { textOffset: 50 + 11 },
        },
        {
            name: 'past a boxless character opening a line',
            body: SPACED,
            y: restoredAt(2 * BLOCK_HEIGHT, 1),
            at: { textOffset: 50 + 20 + 10 },
        },
    ])('captures $name', async ({ body, y, at, innerHeight }) => {
        if (innerHeight)
            Object.defineProperty(window, 'innerHeight', { value: innerHeight, configurable: true })
        withBook(body)
        const { session } = start()
        await flush()
        await userScroll(y)
        await flush()
        await session.dispose()
        expect(lastWrite().progress!.book).toMatchObject(at)
    })

    it.each([
        [
            'an old block-start locator to the block first glyph',
            { textOffset: 2 * 50, anchorId: 'l2' },
            restoredAt(2 * BLOCK_HEIGHT),
        ],
        [
            'a locator into hidden text to the start of its block',
            { textOffset: 3 * 50 + 1, anchorId: 'l3' },
            3 * BLOCK_HEIGHT - 8,
        ],
        [
            'a mid-paragraph locator on its own line',
            { textOffset: 50 + 3 * CHARS_PER_LINE, anchorId: 'l1' },
            restoredAt(BLOCK_HEIGHT, 3),
        ],
    ])('restores %s', async (_name, locator, expected) => {
        withBook(LONG_BODY, locator)
        start()
        await flush()
        expect(scrollY).toBe(expected)
    })

    it('falls back to the block start of vertical text past a straddling slice end', async () => {
        serve(book(['a.xhtml', 'b.xhtml'], ['a.xhtml']), {
            'a.xhtml': doc(`<p>${PARA}</p>`.repeat(10)),
            'b.xhtml': doc(`<p id="v" style="writing-mode: vertical-rl">
                ${PARA}</p>`),
        })
        const { session } = start()
        await flush()
        await userScroll(SLICE_HEIGHT - 10)
        await flush()
        await session.dispose()
        expect(lastWrite().progress!.book).toMatchObject({
            href: 'b.xhtml',
            textOffset: 0,
            anchorId: 'v',
        })
    })
})

describe('disposal', () => {
    it('cancels deferred capture and repositioning', async () => {
        const { session, nav } = await startTimed()
        scrollTo(150)
        await changeText()
        await session.dispose()

        const stamped = JSON.stringify(nav.entries)
        const settled = writes().length
        scrollY = 0
        await vi.advanceTimersByTimeAsync(2000)
        expect(scrollY).toBe(0)
        expect(writes()).toHaveLength(settled)
        expect(JSON.stringify(nav.entries)).toBe(stamped)
    })

    it.each(['', ' behind a write in flight'])(
        'keeps reading on both sides of a text setting change%s',
        async busy => {
            const hanging = deferred<ReadingResponse>()
            if (busy) vi.mocked(readingApi.post).mockImplementationOnce(() => hanging.promise)
            const { session } = await startTimed()
            if (busy) {
                await userScroll(50)
                await vi.advanceTimersByTimeAsync(1500)
            }
            await userScroll(150)
            await changeText()
            await vi.advanceTimersByTimeAsync(300)
            await userScroll(250)
            await vi.advanceTimersByTimeAsync(100)
            hanging.resolve(respond(writes()[0]![1]))
            await vi.advanceTimersByTimeAsync(1500)
            await session.dispose()
            const positions = writes().filter(([, req]) => req.op === 'position')
            expect(positions).toHaveLength(busy ? 3 : 2)
            const [before, after] = positions.slice(-2).map(([, req]) => req)
            expect(after!.seq).toBeGreaterThan(before!.seq!)
            expect(after!.progress).not.toEqual(before!.progress)
        }
    )

    it('sends the closing write after one in flight, later in sequence', async () => {
        const hanging = deferred<ReadingResponse>()
        vi.mocked(readingApi.post).mockImplementationOnce(() => hanging.promise)
        const { session } = await startTimed()

        await userScroll(150)
        await vi.advanceTimersByTimeAsync(1500)
        expect(writes()).toHaveLength(1)

        await userScroll(250)
        session.dispose()
        await flush()
        expect(writes()).toHaveLength(1)
        hanging.resolve(respond(writes()[0]![1]))
        await flush()
        expect(writes()).toHaveLength(2)
        expect(writes()[1]![1].seq).toBeGreaterThan(writes()[0]![1].seq!)
    })

    it('does not leave a navigation waiting for a host that will never arrive', async () => {
        // No boundary needs a document, so the flow document stays unfetched
        // until the return from standalone asks for it.
        const gate = deferred<string>()
        serve(book(['a.xhtml', 'notes.xhtml!']), {
            'a.xhtml': gate.promise,
            'notes.xhtml': DOCUMENTS['notes.xhtml']!,
        })
        const { session } = start({ ch: 'notes.xhtml' })
        await flush()
        expect(session.standalone?.href).toBe('notes.xhtml')

        const returning = session.closeStandalone()
        await flush()
        // The reader unmounts while the destination is still being fetched.
        session.setElements({ host: null, sentinel: null })
        await session.dispose()
        gate.resolve(doc('<p>back in the flow</p>'))

        const outcome = await Promise.race([
            returning.then(() => 'settled'),
            new Promise(resolve => setTimeout(() => resolve('hung'), 100)),
        ])
        expect(outcome).toBe('settled')
    })
})

describe('empty slices', () => {
    it('are whitespace and bare elements, unless a background image draws them', () => {
        const empty = (html: string, styles: string[] = []) => {
            const body = document.createElement('body')
            body.innerHTML = html
            return isEmptySlice(body, styles)
        }
        expect(empty(' <div>\n</div> ', ['p { color: red }'])).toBe(true)
        expect(empty('<div>x</div>')).toBe(false)
        expect(empty('<div><img src="a.png"></div>')).toBe(false)
        expect(empty('<div class="cover"></div>', ['.cover { background: url(a.png) }'])).toBe(
            false
        )
        expect(empty('<div style="background-image: url(a.png)"></div>')).toBe(false)
        expect(empty('', ['@font-face { src: url(a.woff) }'])).toBe(true)
    })
})
