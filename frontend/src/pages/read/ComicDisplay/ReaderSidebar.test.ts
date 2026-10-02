import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { reactive, ref } from 'vue'
import type { Siblings } from '../useSiblings'
import ReaderSidebar from './ReaderSidebar.vue'

const reader = vi.hoisted(() => ({ value: null as unknown as Record<string, unknown> }))
vi.mock('./useComicDisplayStore', () => ({ useReaderStore: () => reader.value }))
vi.mock('@/pages/_layout/useLayoutStore', () => ({
    useLayoutStore: () => ({ navbarHidden: { useLayer: () => () => {} } }),
}))
vi.mock('@/utils/api/content', () => ({
    contentApi: { useGet: () => ({ data: ref({ id: 's_1', title: 'Series' }) }) },
}))

function render(parentId: string | null, siblings: Partial<Siblings>) {
    const retry = vi.fn()
    reader.value = reactive({
        sidebarOpen: true,
        state: {
            contentId: 'c_1',
            content: { id: 'c_1', parent_id: parentId, title: 'Comic' },
            loading: false,
            page: 0,
            pageDimensions: [{ width: 1, height: 1 }],
        },
        sync: null,
        siblings: {
            status: 'ready',
            items: [],
            index: -1,
            prev: null,
            next: null,
            retry,
            ...siblings,
        },
        seriesSettings: {},
        settings: { longstripWidth: 100 },
        mode: 'paged',
    })
    const wrapper = mount(ReaderSidebar, {
        global: {
            stubs: {
                ADrawer: { template: '<div><slot title-id="t" /></div>' },
                AIconButton: { props: ['label'], template: '<button>{{ label }}</button>' },
                AButton: { template: '<button><slot /></button>' },
                ACombobox: { props: ['label'], template: '<div>{{ label }}</div>' },
                ASlider: true,
                ASegmented: true,
            },
        },
    })
    return { wrapper, retry }
}

describe('ReaderSidebar', () => {
    it('has no chapter controls for a standalone comic', () => {
        const { wrapper } = render(null, {})
        expect(wrapper.text()).not.toContain('Chapter')
        expect(wrapper.text()).not.toContain('Retry')
    })

    it('shows the chapter controls of a series', () => {
        const { wrapper } = render('s_1', { status: 'ready', index: 0, items: [{}, {}] as never })
        expect(wrapper.text()).toContain('Chapter')
        expect(wrapper.text()).toContain('1 of 2')
    })

    it("offers Retry when the series' volumes failed", async () => {
        const { wrapper, retry } = render('s_1', { status: 'error' })
        expect(wrapper.text()).not.toContain('Chapter')
        await wrapper
            .findAll('button')
            .find(b => b.text() === 'Retry')!
            .trigger('click')
        expect(retry).toHaveBeenCalled()
    })
})
