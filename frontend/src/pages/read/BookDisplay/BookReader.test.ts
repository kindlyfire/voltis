import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia, type Pinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { contentApi } from '@/utils/api/content'
import type { BookStructure, Content, UserToContent } from '@/utils/api/types'
import BookReader from './BookReader.vue'
import { useBookDisplayStore } from './useBookDisplayStore'

vi.mock('@/utils/api/content', () => ({
    contentApi: {
        get: vi.fn(),
        bookStructure: vi.fn(),
        bookDocument: vi.fn(),
        updateUserData: vi.fn(),
        useGet: () => ({ data: { value: undefined } }),
    },
    bumpRecentlyRead: vi.fn(),
    invalidateRecentlyRead: vi.fn(),
    invalidateStatusChange: vi.fn(),
}))

// Real, it would pull in Vue Query with no provider.
vi.mock('../useReaderTutorial', () => ({ useReaderTutorial: vi.fn() }))
vi.mock('./useNextVolume', async () => {
    const { ref } = await import('vue')
    return { useNextVolume: () => ref(null) }
})

const stubs = {
    AButton: { template: '<button><slot /></button>' },
    ADivider: { template: '<hr />' },
    ASpinner: { template: '<div class="loading" />' },
    AAlert: { template: '<div class="alert"><slot /></div>' },
    AProgressBar: { template: '<div class="progress" />' },
    BookReaderDrawer: true,
}

const STRUCTURE: BookStructure = {
    spine: [
        { href: 'a.xhtml', title: 'A', linear: true, words: 10 },
        { href: 'b.xhtml', title: 'B', linear: true, words: 10 },
    ],
    toc: [
        { id: 'c1', title: 'One', depth: 0, href: 'a.xhtml', fragment: '' },
        { id: 'c2', title: 'Two', depth: 0, href: 'b.xhtml', fragment: '' },
    ],
}

let pinia: Pinia
let router: Router

beforeEach(async () => {
    // Stamped by the last test's session, it would be restored by this one's.
    history.replaceState(null, '')
    pinia = createPinia()
    setActivePinia(pinia)
    router = createRouter({
        history: createMemoryHistory(),
        routes: [{ path: '/r/:id', component: { template: '<div />' } }],
    })
    router.push('/r/c_1')
    await router.isReady()

    vi.mocked(contentApi.get).mockResolvedValue({
        id: 'c_1',
        file_mtime: '2026-01-01',
        title: 'A Book',
        type: 'book',
        user_data: null,
    } as unknown as Content)
    vi.mocked(contentApi.bookStructure).mockResolvedValue(STRUCTURE)
    vi.mocked(contentApi.updateUserData).mockResolvedValue({ progress: {} } as UserToContent)
    window.scrollTo = vi.fn() as unknown as typeof window.scrollTo
    vi.stubGlobal(
        'ResizeObserver',
        class {
            observe() {}
            disconnect() {}
        }
    )
    Object.defineProperty(Range.prototype, 'getBoundingClientRect', {
        value: () => ({ top: 0, bottom: 0, left: 0, right: 0, width: 0, height: 0 }),
        configurable: true,
    })
})

let wrapper: ReturnType<typeof mount>

afterEach(async () => {
    wrapper.unmount()
    await useBookDisplayStore(pinia).dispose()
})

/** The reader, showing `contentId` in `mode`, once its loads have settled. */
async function open(mode: 'paged' | 'scroll', contentId = 'c_1') {
    wrapper = mount(BookReader, {
        props: { contentId },
        global: { plugins: [pinia, router], stubs },
        attachTo: document.body,
    })
    const store = useBookDisplayStore(pinia)
    store.settings.mode = mode
    store.setContent(contentId, { ch: null, frag: null })
    await settle()
    return store
}

/** Each mounted slice lives in its own shadow root under the host. */
function mountedText(): string {
    return Array.from(wrapper.find('.book-host').element.children)
        .map(child => child.shadowRoot?.textContent ?? '')
        .join('')
}

function deferred<T>() {
    let resolve!: (value: T) => void
    let reject!: (reason?: unknown) => void
    const promise = new Promise<T>((res, rej) => {
        resolve = res
        reject = rej
    })
    promise.catch(() => {})
    return { promise, resolve, reject }
}

async function settle(times = 4) {
    for (let i = 0; i < times; i++) await flushPromises()
}

function oneDocument(href: string): BookStructure {
    return {
        spine: [{ href, title: href, linear: true, words: 10 }],
        toc: [{ id: 'c1', title: 'One', depth: 0, href, fragment: '' }],
    }
}

describe('BookReader host lifecycle', () => {
    it('renders a chapter selected after the first one failed to load', async () => {
        vi.mocked(contentApi.bookDocument).mockImplementation(async (_id, href) => {
            if (href === 'a.xhtml') throw new Error('500')
            return '<html><body><p id="p">second chapter</p></body></html>'
        })

        const store = await open('scroll')
        expect(store.session!.error).toBeTruthy()
        expect(wrapper.text()).toContain('500')
        // The host must survive the error, or nothing can ever render again.
        expect(wrapper.find('.book-host').exists()).toBe(true)

        store.session!.setEntry({ ch: 'b.xhtml', frag: null })
        await settle()

        expect(store.session!.error).toBeNull()
        expect(store.session!.restoring).toBe(false)
        expect(store.session!.chapterIndex).toBe(1)
        expect(mountedText()).toContain('second chapter')
        // The sentinel comes back with the chapter chrome, so completion can work.
        expect(wrapper.find('.h-px').exists()).toBe(true)
        expect(wrapper.text()).toContain('Previous chapter')
        expect(wrapper.text()).toContain('End of book')
    })
})

describe('BookReader across books', () => {
    it('drops the previous book content while the next one loads and fails', async () => {
        const gate = deferred<string>()
        vi.mocked(contentApi.get).mockImplementation(
            async id =>
                ({
                    id,
                    file_mtime: '2026-01-01',
                    title: id,
                    type: 'book',
                    user_data: null,
                }) as unknown as Content
        )
        vi.mocked(contentApi.bookStructure).mockImplementation(async id =>
            oneDocument(id === 'c_a' ? 'a.xhtml' : 'b.xhtml')
        )
        vi.mocked(contentApi.bookDocument).mockImplementation((_id, href) =>
            href === 'a.xhtml'
                ? Promise.resolve('<html><body><p>book a text</p></body></html>')
                : gate.promise
        )

        const store = await open('scroll', 'c_a')
        expect(mountedText()).toContain('book a text')
        expect(wrapper.find('.h-px').exists()).toBe(true)

        // The reader stays mounted; only the book changes.
        store.setContent('c_b', { ch: null, frag: null })
        await settle()
        expect(store.session!.contentId).toBe('c_b')
        expect(mountedText()).not.toContain('book a text')

        gate.reject(new Error('500'))
        await settle()
        expect(store.session!.error).toBeTruthy()
        expect(mountedText()).not.toContain('book a text')
    })
})

describe('BookReader layouts', () => {
    beforeEach(() => {
        vi.mocked(contentApi.bookDocument).mockImplementation(
            async (_id, href) => `<html><body><p>in ${href}</p></body></html>`
        )
    })

    function key(name: string) {
        window.dispatchEvent(new KeyboardEvent('keydown', { key: name }))
    }

    it('pages with a counter and no chapter buttons or progress bar', async () => {
        const session = (await open('paged')).session!
        expect(session.layoutMode).toBe('paged')
        expect(wrapper.find('.book-reader').classes()).toContain('is-paged')
        expect(wrapper.find('.book-footer').text()).toContain('Page 1 / 1')
        expect(wrapper.text()).not.toContain('Next chapter')
        expect(wrapper.find('.progress').exists()).toBe(false)
        expect(wrapper.find('.h-px').exists()).toBe(false)

        const turn = vi.spyOn(session, 'turn')
        key('ArrowRight')
        key('ArrowUp')
        await wrapper.findAll('.page-button')[1]!.trigger('click')
        expect(turn.mock.calls).toEqual([['next'], ['prev'], ['next']])
        turn.mockRestore()

        session.setEntry({ ch: 'b.xhtml', frag: null })
        await settle()
        session.turn('next')
        await settle()
        expect(session.atBookEnd).toBe(true)
        expect(wrapper.text()).toContain('End of book')
        expect(wrapper.find('.book-footer').text()).not.toContain('Page')
    })

    it('scrolls with chapter buttons and a chapter progress bar', async () => {
        const store = await open('scroll')
        expect(store.session!.layoutMode).toBe('scroll')
        expect(wrapper.find('.book-footer').exists()).toBe(false)
        expect(wrapper.text()).toContain('Next chapter')
        const describedBy = wrapper.find('button').attributes('aria-describedby')
        expect(wrapper.find(`[id="${describedBy}"]`).text()).toBe('Two')
        expect(wrapper.find('.progress').exists()).toBe(true)
    })
})
