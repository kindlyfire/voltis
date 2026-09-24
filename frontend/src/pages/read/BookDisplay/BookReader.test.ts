import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia, type Pinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
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
    },
}))

// Real, it would pull in Vue Query with no provider.
vi.mock('../useReaderTutorial', () => ({ useReaderTutorial: vi.fn() }))

const stubs = {
    AButton: { template: '<button><slot /></button>' },
    ADivider: { template: '<hr />' },
    ASpinner: { template: '<div class="loading" />' },
    AAlert: { template: '<div class="alert"><slot /></div>' },
    AProgressBar: { template: '<div />' },
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
})

function render() {
    return mount(BookReader, {
        props: { contentId: 'c_1' },
        global: { plugins: [pinia, router], stubs },
        attachTo: document.body,
    })
}

// The host is the only element whose sole class is `flex-1`; the page wrapper
// carries `flex flex-1 flex-col`.
function hostOf(wrapper: ReturnType<typeof render>) {
    return wrapper.find('[class="flex-1"]')
}

/** Each mounted slice lives in its own shadow root under the host. */
function mountedText(wrapper: ReturnType<typeof render>): string {
    const host = hostOf(wrapper).element as HTMLElement
    return Array.from(host.children)
        .map(child => (child as HTMLElement).shadowRoot?.textContent ?? '')
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

        const wrapper = render()
        const store = useBookDisplayStore(pinia)
        store.setContent('c_1', { ch: null, frag: null })
        await settle()

        expect(store.session!.error).toBeTruthy()
        expect(wrapper.text()).toContain('500')
        // The host must survive the error, or nothing can ever render again.
        expect(hostOf(wrapper).exists()).toBe(true)

        store.session!.setEntry({ ch: 'b.xhtml', frag: null })
        await settle()

        expect(store.session!.error).toBeNull()
        expect(store.session!.restoring).toBe(false)
        expect(store.session!.pageIndex).toBe(1)
        expect(mountedText(wrapper)).toContain('second chapter')
        // The sentinel comes back with the page chrome, so completion can work.
        expect(wrapper.find('.h-px').exists()).toBe(true)
        expect(wrapper.text()).toContain('Previous: One')
        expect(wrapper.text()).toContain('End of book')

        wrapper.unmount()
        await store.dispose()
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

        const wrapper = render()
        const store = useBookDisplayStore(pinia)
        store.setContent('c_a', { ch: null, frag: null })
        await settle()
        expect(mountedText(wrapper)).toContain('book a text')
        expect(wrapper.find('.h-px').exists()).toBe(true)

        // The reader stays mounted; only the book changes.
        store.setContent('c_b', { ch: null, frag: null })
        await settle()
        expect(store.session!.contentId).toBe('c_b')
        expect(mountedText(wrapper)).not.toContain('book a text')

        gate.reject(new Error('500'))
        await settle()
        expect(store.session!.error).toBeTruthy()
        expect(mountedText(wrapper)).not.toContain('book a text')

        wrapper.unmount()
        await store.dispose()
    })
})
