import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, reactive, ref, watch } from 'vue'
import { contentApi } from '@/utils/api/content'
import type { BookLocator, BookStructure, Content, UserToContent } from '@/utils/api/types'
import type { BookEntry } from './bookEntry'
import { zBookSettings, type BookSettings } from './bookSettings'
import { mountReader, words } from './browserFixture'
import { createBookSession, type BookSession } from './createBookSession'
import { FakeNav } from './fakeNav'
import { locateIn } from './locatorGeometry'
import type { LayoutKind } from './readingLayout'

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
        { href: 'a.xhtml', title: 'A', linear: true, words: 600 },
        { href: 'b.xhtml', title: 'B', linear: true, words: 600 },
        { href: 'c.xhtml', title: 'C', linear: true, words: 600 },
        { href: 'notes.xhtml', title: 'Notes', linear: false, words: 10 },
    ],
    toc: ['a', 'b', 'c'].map(id => ({
        id,
        title: id.toUpperCase(),
        depth: 0,
        href: `${id}.xhtml`,
        fragment: '',
    })),
}

const BASE = '<style>body { margin: 0 } p { margin: 0 }</style>'
const chapter = (id: string, head = '') =>
    `<html><head>${BASE}${head}</head><body><p id="${id}">${words(600, id)}</p>` +
    `<p><a href="notes.xhtml">note</a></p></body></html>`
const DOCS: Record<string, string> = {
    'a.xhtml': chapter('a'),
    'b.xhtml': chapter('b'),
    'c.xhtml': chapter('c'),
    'notes.xhtml': `<html><head>${BASE}</head><body><p>${words(20, 'n')}</p></body></html>`,
}

function content(progress: Record<string, unknown> = {}): Content {
    return {
        id: 'c_1',
        file_mtime: '2026-01-01',
        title: 'A Book',
        type: 'book',
        user_data: { status: 'reading', progress },
    } as unknown as Content
}

function deferred<T>() {
    let resolve!: (value: T) => void
    const promise = new Promise<T>(res => (resolve = res))
    return { promise, resolve }
}

const frames = async (n = 2) => {
    for (let i = 0; i < n; i++) await new Promise(resolve => requestAnimationFrame(resolve))
}

let cleanup: (() => Promise<void>) | null = null

beforeEach(() => {
    vi.mocked(contentApi.get).mockResolvedValue(content())
    vi.mocked(contentApi.bookStructure).mockResolvedValue(STRUCTURE)
    vi.mocked(contentApi.bookDocument).mockImplementation(async (_id, href) => DOCS[href]!)
    vi.mocked(contentApi.updateUserData).mockResolvedValue({ progress: {} } as UserToContent)
})

afterEach(async () => {
    await cleanup?.()
    cleanup = null
    Reflect.deleteProperty(document, 'fonts')
    window.scrollTo({ top: 0, behavior: 'instant' })
})

/** Holds settling open: the session waits for fonts before its final placement. */
function holdFonts() {
    const fonts = deferred<void>()
    Object.defineProperty(document, 'fonts', {
        value: { ready: fonts.promise, addEventListener() {}, removeEventListener() {} },
        configurable: true,
    })
    return () => fonts.resolve()
}

/** A session in the reader DOM, which follows the layout and the font size
 * the way `BookReader.vue` renders them. */
async function open(entry: Partial<BookEntry> = {}, { settle = true } = {}) {
    const dom = mountReader()
    dom.reader.style.setProperty('--reader-width', '20')
    const settings = reactive<BookSettings>({
        ...zBookSettings.parse({}),
        mode: 'paged',
        spread: '1',
    })
    const nav = new FakeNav()
    const session = createBookSession('c_1', { ch: null, frag: null, ...entry }, nav, ref(settings))
    nav.session = session
    const stop = watch(
        () => [session.layoutMode, settings.fontSize] as const,
        ([mode, size]) => {
            dom.reader.classList.toggle('is-paged', mode === 'paged')
            dom.reader.style.setProperty('--reader-font-size', `${size * 16}px`)
        },
        { immediate: true, flush: 'post' }
    )
    session.setElements({ host: dom.host, sentinel: null })
    cleanup = async () => {
        stop()
        await session.dispose()
        dom.dispose()
    }
    if (settle) await settled(session)
    return { session, nav, settings, ...dom }
}

function settled(session: BookSession, chapterIndex?: number) {
    return vi.waitFor(() => {
        expect(session.firstChapterMounted).toBe(true)
        if (chapterIndex != null) expect(session.chapterIndex).toBe(chapterIndex)
        expect(session.restoring).toBe(false)
    })
}

async function setMode(settings: BookSettings, mode: LayoutKind) {
    settings.mode = mode
    await nextTick()
}

/** The passage the session would store, stamped onto the current entry. */
function passage(session: BookSession, nav: FakeNav): BookLocator {
    session.snapshotPassage()
    return nav.historyLocator()!
}

/** Whether the locator's glyph is on screen: inside the paged frame, or the
 * window in scroll mode. */
function onScreen(host: HTMLElement, locator: BookLocator) {
    const slices = Array.from(host.querySelectorAll('.book-slice')).map(el => ({
        slice: { href: locator.href },
        root: el.shadowRoot!.querySelector('html')!,
        startOffset: 0,
    }))
    const rect = locateIn(slices, locator)!.getBoundingClientRect()
    if (host.closest('.is-paged')) {
        const frame = host.getBoundingClientRect()
        return rect.left >= frame.left && rect.right <= frame.right
    }
    return rect.top >= 0 && rect.bottom <= window.innerHeight
}

async function turnTo(session: BookSession, index: number) {
    while (session.screen!.index < index) session.turn('next')
    await frames()
}

describe('paged session', () => {
    it('turns within a chapter, then crosses with one push to the next chapter', async () => {
        const { session, nav } = await open()
        expect(session.layoutMode).toBe('paged')
        const count = session.screen!.count
        expect(count).toBeGreaterThanOrEqual(3)

        session.turn('next')
        expect(session.screen!.index).toBe(1)
        expect(passage(session, nav).textOffset).toBeGreaterThan(0)

        await turnTo(session, count - 1)
        const pushes = nav.entries.length
        session.turn('next')
        session.turn('next')
        await settled(session, 1)
        expect(session.screen!.index).toBe(0)
        expect(nav.entries.length).toBe(pushes + 1)
    })

    it('lands on the last screen going back, and Back returns to where it was', async () => {
        const { session, nav, host } = await open({ ch: 'c.xhtml' })
        session.turn('next')
        session.turn('prev')
        const left = passage(session, nav)
        session.turn('prev')
        await settled(session, 1)
        expect(session.screen!.index).toBe(session.screen!.count - 1)

        nav.go(-1)
        await settled(session, 2)
        expect(session.screen!.index).toBe(0)
        expect(onScreen(host, left)).toBe(true)
    })

    it('re-measures before crossing, so text added since skips nothing', async () => {
        const { session, host } = await open()
        await turnTo(session, session.screen!.count - 1)
        const last = session.screen!.index
        const body = host.querySelector('.book-slice')!.shadowRoot!.querySelector('body')!
        body.insertAdjacentHTML('beforeend', `<p>${words(300, 'x')}</p>`)
        session.turn('next')
        expect(session.chapterIndex).toBe(0)
        expect(session.screen!.index).toBe(last + 1)
    })

    it('keeps the passage through a font-size relayout', async () => {
        const { session, nav, host, settings } = await open()
        await turnTo(session, 2)
        const before = passage(session, nav)
        settings.fontSize = 1.4
        await frames(3)
        expect(onScreen(host, before)).toBe(true)
        expect(passage(session, nav)).toEqual(before)
    })

    it('restores a saved locator to the screen containing it', async () => {
        const saved: BookLocator = { version: 1, href: 'a.xhtml', textOffset: 'a0 '.length * 400 }
        vi.mocked(contentApi.get).mockResolvedValue(content({ book: saved }))
        const { session, host } = await open()
        expect(session.screen!.index).toBeGreaterThan(0)
        expect(onScreen(host, saved)).toBe(true)
    })

    it('lets the reader take over while the chapter settles', async () => {
        const release = holdFonts()
        const saved: BookLocator = { version: 1, href: 'a.xhtml', textOffset: 'a0 '.length * 300 }
        vi.mocked(contentApi.get).mockResolvedValue(content({ book: saved }))
        const { session } = await open({}, { settle: false })
        await vi.waitFor(() => expect(session.firstChapterMounted).toBe(true))
        expect(session.restoring).toBe(true)
        const landed = session.screen!.index

        session.turn('next')
        expect(session.screen!.index).toBe(landed + 1)
        release()
        await settled(session)
        expect(session.screen!.index).toBe(landed + 1)
    })
    it('saves where the reader took over if the tab closes before settling', async () => {
        holdFonts()
        const saved: BookLocator = { version: 1, href: 'a.xhtml', textOffset: 'a0 '.length * 300 }
        vi.mocked(contentApi.get).mockResolvedValue(content({ book: saved }))
        const { session } = await open({}, { settle: false })
        await vi.waitFor(() => expect(session.firstChapterMounted).toBe(true))
        session.turn('next')
        await session.dispose()
        const written = vi.mocked(contentApi.updateUserData).mock.calls.at(-1)![1].progress!.book!
        expect(written.textOffset).toBeGreaterThan(saved.textOffset)
    })

    it('lays out again for a text setting changed while the chapter settles', async () => {
        const release = holdFonts()
        const { session, settings, viewport } = await open({}, { settle: false })
        await vi.waitFor(() => expect(session.firstChapterMounted).toBe(true))
        const columnWidth = () => parseInt(viewport.style.getPropertyValue('--pg-col-w'))
        const before = columnWidth()
        settings.fontSize = 1.4
        await frames()
        release()
        await settled(session)
        expect(columnWidth()).toBeGreaterThan(before)
    })
})

describe('paged turns during a route change', () => {
    it('are stamped on their entry before a push leaves it', async () => {
        const { session, nav } = await open()
        session.turn('next')
        nav.push({ href: 'c.xhtml', fragment: '' })
        await settled(session, 2)
        nav.go(-1)
        await settled(session, 0)
        expect(session.screen!.index).toBe(1)
    })

    it('are dropped until the routed chapter commits', async () => {
        const { session, nav } = await open()
        // As a Contents link does: the route moves, then the session follows.
        nav.push({ href: 'c.xhtml', fragment: '' })
        session.turn('next')
        session.turn('next')
        session.goToChapter(1)
        expect(nav.entries.map(entry => entry.target.href)).toEqual(['a.xhtml', 'c.xhtml'])
        await settled(session, 2)
        expect(session.screen!.index).toBe(0)
        session.turn('next')
        expect(session.screen!.index).toBe(1)
    })
})

describe('paged end of book', () => {
    it('completes only by turning onto the end screen, which navigation leaves', async () => {
        const { session } = await open({ ch: 'c.xhtml' })
        await turnTo(session, session.screen!.count - 1)
        window.dispatchEvent(new Event('pointerdown'))
        expect(session.atBookEnd).toBe(false)

        session.turn('next')
        expect(session.atBookEnd).toBe(true)
        session.turn('next')
        expect(session.chapterIndex).toBe(2)
        session.turn('prev')
        expect(session.atBookEnd).toBe(false)
        expect(session.screen!.index).toBe(session.screen!.count - 1)

        session.turn('next')
        expect(session.atBookEnd).toBe(true)
        session.goToChapter(0)
        expect(session.atBookEnd).toBe(false)
        await settled(session, 0)
        await session.dispose()
        expect(vi.mocked(contentApi.updateUserData).mock.calls.at(-1)![1].status).toBe('completed')
    })

    it('is left by a mode change', async () => {
        const { session, settings } = await open({ ch: 'c.xhtml' })
        await turnTo(session, session.screen!.count - 1)
        session.turn('next')
        expect(session.atBookEnd).toBe(true)
        await setMode(settings, 'scroll')
        expect(session.atBookEnd).toBe(false)
    })
})

describe('paged standalone', () => {
    it('returns to the passage when turned past its end', async () => {
        const { session, host } = await open()
        session.turn('next')
        const link = host
            .querySelector('.book-slice')!
            .shadowRoot!.querySelector('[data-book-href="notes.xhtml"]') as HTMLElement
        link.click()
        await vi.waitFor(() => expect(session.standalone?.href).toBe('notes.xhtml'))
        await settled(session)

        session.turn('next')
        await vi.waitFor(() => expect(session.standalone).toBeNull())
        await settled(session)
        expect(session.screen!.index).toBe(1)
    })
})

describe('switching layouts', () => {
    it('keeps the passage from paged to scroll and back', async () => {
        const { session, nav, host, settings } = await open()
        await turnTo(session, 2)
        const before = passage(session, nav)

        await setMode(settings, 'scroll')
        await frames()
        expect(session.layoutMode).toBe('scroll')
        expect(onScreen(host, before)).toBe(true)

        await setMode(settings, 'paged')
        await frames()
        expect(session.screen!.index).toBe(2)
        expect(onScreen(host, before)).toBe(true)
    })

    it('keeps the passage through rapid switches', async () => {
        const { session, nav, host, settings } = await open()
        await turnTo(session, 2)
        const before = passage(session, nav)
        // A microtask apart: no frame runs between them.
        await setMode(settings, 'scroll')
        await setMode(settings, 'paged')
        await setMode(settings, 'scroll')
        await frames()
        expect(onScreen(host, before)).toBe(true)
        expect(passage(session, nav)).toEqual(before)
    })

    it('lands on the target when switched while the chapter settles', async () => {
        const release = holdFonts()
        const saved: BookLocator = { version: 1, href: 'a.xhtml', textOffset: 'a0 '.length * 300 }
        vi.mocked(contentApi.get).mockResolvedValue(content({ book: saved }))
        const { session, host, settings } = await open({}, { settle: false })
        await vi.waitFor(() => expect(session.firstChapterMounted).toBe(true))
        await setMode(settings, 'scroll')
        await frames()
        release()
        await settled(session)
        expect(session.layoutMode).toBe('scroll')
        expect(onScreen(host, saved)).toBe(true)

        await setMode(settings, 'paged')
        await frames()
        expect(onScreen(host, saved)).toBe(true)
    })

    it('scrolls a chapter with vertical writing', async () => {
        vi.mocked(contentApi.bookDocument).mockImplementation(async (_id, href) =>
            href === 'b.xhtml'
                ? chapter('b', '<style>html { writing-mode: vertical-rl }</style>')
                : DOCS[href]!
        )
        const { session } = await open({ ch: 'b.xhtml' })
        expect(session.layoutMode).toBe('scroll')
        expect(session.screen).toBeNull()

        session.goToChapter(2)
        await settled(session, 2)
        await frames()
        expect(session.layoutMode).toBe('paged')
        expect(session.screen!.count).toBeGreaterThan(1)
    })
})
