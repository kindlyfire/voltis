import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import MarkSeriesCompletedModal from './MarkSeriesCompletedModal.vue'

const fetch = vi.fn<(url: string) => Promise<Response>>()
vi.stubGlobal('fetch', fetch)
afterEach(() => fetch.mockReset())

const stubs = {
    ADialog: { template: '<div><slot /><slot name="actions" /></div>' },
    AButton: {
        props: ['disabled'],
        emits: ['click'],
        template: '<button :disabled="disabled" @click="$emit(\'click\')"><slot /></button>',
    },
    AAlert: { template: '<div><slot /></div>' },
    ACheckbox: { props: ['label'], template: '<label>{{ label }}</label>' },
}

function render(props: Record<string, unknown>) {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    return mount(MarkSeriesCompletedModal, {
        props: { open: true, close: vi.fn(), seriesId: 's_1', ...props },
        global: { plugins: [[VueQueryPlugin, { queryClient }]], stubs },
    })
}
const confirm = (w: ReturnType<typeof render>) =>
    w.findAll('button').find(b => b.text() === 'Mark completed')!

it('offers the unread volumes from counts the reader passes, without fetching', async () => {
    const wrapper = render({ known: { type: 'book_series', unread: 2 } })
    await flushPromises()
    expect(fetch).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Also mark 2 unread volumes as read')
    expect(confirm(wrapper).attributes('disabled')).toBeUndefined()
})

it('waits for the series elsewhere, and shows why when it fails', async () => {
    fetch.mockResolvedValueOnce(new Response('down', { status: 503 }))
    const wrapper = render({})
    await flushPromises()
    expect(confirm(wrapper).attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('Server error (503)')

    fetch.mockResolvedValueOnce(Response.json({ type: 'comic_series', unread_children_count: 3 }))
    await wrapper
        .findAll('button')
        .find(b => b.text() === 'Retry')!
        .trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Also mark 3 unread chapters as read')
    expect(confirm(wrapper).attributes('disabled')).toBeUndefined()
})
