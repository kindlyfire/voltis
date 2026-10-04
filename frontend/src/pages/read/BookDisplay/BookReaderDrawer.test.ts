import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia, type Pinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, reactive, ref } from 'vue'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import type { BookStructure } from '@/utils/api/types'
import { addOverlays } from '@/utils/modalTesting'
import BookReaderDrawer from './BookReaderDrawer.vue'
import type { BookSession } from './createBookSession'
import { useBookDisplayStore } from './useBookDisplayStore'

vi.mock('@/utils/api/content', () => ({
    contentApi: { useGet: () => ({ data: ref(undefined) }) },
}))

const STRUCTURE: BookStructure = {
    spine: Array.from({ length: 20 }, (_, i) => ({
        href: `${i}.xhtml`,
        title: `${i}`,
        linear: true,
        words: 10,
    })),
    toc: Array.from({ length: 20 }, (_, i) => ({
        id: `c${i}`,
        title: `Chapter ${i}`,
        depth: 0,
        href: `${i}.xhtml`,
        fragment: '',
    })),
}

let pinia: Pinia
let router: Router

beforeEach(async () => {
    addOverlays()
    window.matchMedia = vi.fn(() => ({
        matches: false,
        addEventListener() {},
        removeEventListener() {},
    })) as unknown as typeof window.matchMedia
    pinia = createPinia()
    setActivePinia(pinia)
    router = createRouter({
        history: createMemoryHistory(),
        routes: [{ path: '/:p(.*)', component: { template: '<div />' } }],
    })
    router.push('/r/c_1')
    await router.isReady()
})

afterEach(() => {
    vi.restoreAllMocks()
    document.body.innerHTML = ''
})

function fakeSession(structure: BookStructure | null = STRUCTURE) {
    return reactive({
        contentId: 'c_1',
        content: { title: 'A Book' },
        chapters: STRUCTURE.spine,
        chapterIndex: 12,
        title: 'Chapter 12',
        percent: 60,
        prevChapter: true,
        nextChapter: true,
        goToChapter: vi.fn(),
        structure,
        entryChapters: Object.fromEntries(STRUCTURE.toc.map((e, i) => [e.id, i])),
        chapter: { target: { href: '12.xhtml' } },
        fallback: false,
        snapshotPassage: vi.fn(),
        dispose: vi.fn(),
        layoutMode: 'paged',
        sync: {
            acked: { status: 'reading' },
            series: null,
            tracking: true,
            command: vi.fn(),
            seriesCommand: vi.fn(),
        },
    })
}

function render(session = fakeSession()) {
    const wrapper = mount(BookReaderDrawer, {
        props: { exit: { to: '/c_1', label: 'Back to the book' } },
        global: {
            plugins: [pinia, router],
            stubs: { BookSettings: true, AIconButton: { template: '<button />' } },
        },
        attachTo: document.body,
    })
    const store = useBookDisplayStore()
    store.session = session as unknown as BookSession
    return { wrapper, store, session }
}

const tab = (name: string) =>
    [...document.querySelectorAll<HTMLElement>('[role=tab]')].find(t => t.textContent === name)!
const selectedTab = () => document.querySelector('[role=tab][aria-selected=true]')?.textContent
const panel = (name: string) =>
    document.getElementById(tab(name).getAttribute('aria-controls')!) as HTMLElement

async function setOpen(open: boolean) {
    useBookDisplayStore().sidebarOpen = open
    await nextTick()
}

async function pick(name: string) {
    tab(name).dispatchEvent(new MouseEvent('mousedown', { button: 0 }))
    await nextTick()
}

/** A 200px panel at 100, with the current chapter's entry at 500 (centered: scrollTop 310). */
function mockLayout() {
    vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(200)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (
        this: HTMLElement
    ) {
        const top = this.getAttribute('aria-current') === 'page' ? 500 : 100
        return { top, height: 20 } as DOMRect
    })
}

describe('BookReaderDrawer tabs', () => {
    it('opens on Contents, then on the tab last shown', async () => {
        const { store } = render()
        await setOpen(true)
        expect(selectedTab()).toBe('Contents')

        await pick('Settings')
        expect(store.drawerTab).toBe('settings')
        await setOpen(false)
        await setOpen(true)
        expect(selectedTab()).toBe('Settings')
    })

    it('keeps Settings scroll across tab switches and reopening, but recenters Contents', async () => {
        mockLayout()
        const { store } = render()
        await setOpen(true)
        panel('Contents').scrollTop = 120
        await pick('Settings')
        panel('Settings').scrollTop = 40
        await setOpen(false)
        expect(store.settingsScroll).toBe(40)

        await setOpen(true)
        expect(panel('Settings').scrollTop).toBe(40)
        await pick('Contents')
        expect(panel('Contents').scrollTop).toBe(310)
    })

    it('resets on dispose', async () => {
        const { store } = render()
        await setOpen(true)
        await pick('Settings')
        await setOpen(false)
        void store.dispose()
        expect(store.drawerTab).toBe('contents')
        expect(store.settingsScroll).toBeNull()
    })

    it('centers Contents on the current chapter whenever shown, once loaded', async () => {
        mockLayout()
        const { session } = render(fakeSession(null))
        await setOpen(true)
        expect(panel('Contents').scrollTop).toBe(0)

        session.structure = STRUCTURE
        await nextTick()
        // 400 below the panel's top, less half the leftover height.
        expect(panel('Contents').scrollTop).toBe(310)

        // Centered again on reopening, rather than restored.
        panel('Contents').scrollTop = 30
        await setOpen(false)
        session.chapterIndex = 3
        await setOpen(true)
        expect(panel('Contents').scrollTop).toBe(310)
    })

    it('still centers Contents when it was hidden before the structure loaded', async () => {
        mockLayout()
        const { session } = render(fakeSession(null))
        await setOpen(true)
        await pick('Settings')
        session.structure = STRUCTURE
        await nextTick()
        await pick('Contents')
        expect(panel('Contents').scrollTop).toBe(310)
    })
})

describe('BookReaderDrawer status row', () => {
    it("shows the item's status and marks it completed through the sync", async () => {
        const { session } = render()
        await setOpen(true)
        const row = document.querySelector('.a-drawer')!
        expect(row.textContent).toContain('Reading')
        const button = [...row.querySelectorAll('button')].find(
            b => b.textContent?.trim() === 'Mark completed'
        )!
        button.click()
        expect(session.sync.command).toHaveBeenCalledWith({ op: 'mark_completed' })
    })
})
