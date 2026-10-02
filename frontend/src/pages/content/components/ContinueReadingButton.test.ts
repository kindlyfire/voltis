import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { apiFetch } from '@/utils/fetch'
import { showClearReadingModal } from './ClearReadingModal.vue'
import ContinueReadingButton from './ContinueReadingButton.vue'

const router = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => router }))
vi.mock('./ClearReadingModal.vue', () => ({ showClearReadingModal: vi.fn(async () => true) }))
vi.mock('@/utils/fetch', async importOriginal => ({
    ...(await importOriginal<typeof import('@/utils/fetch')>()),
    apiFetch: vi.fn(),
}))

beforeEach(() => {
    vi.mocked(apiFetch).mockReset()
    vi.mocked(showClearReadingModal).mockClear()
    router.push.mockClear()
})

function render() {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    return mount(ContinueReadingButton, {
        props: { contentId: 's_1', type: 'book_series' },
        global: {
            plugins: [[VueQueryPlugin, { queryClient }]],
            stubs: {
                AButton: {
                    props: ['loading', 'disabled'],
                    template:
                        '<button :data-loading="loading" :disabled="disabled"><slot /></button>',
                },
            },
        },
    })
}

it('shows a failed lookup with Retry, which loads it', async () => {
    vi.mocked(apiFetch).mockRejectedValueOnce(new Error('down'))
    const wrapper = render()
    expect(wrapper.find('button').attributes('data-loading')).toBe('true')
    await flushPromises()
    expect(wrapper.text()).toContain('Retry')
    expect(wrapper.find('button').attributes('data-loading')).not.toBe('true')

    vi.mocked(apiFetch).mockResolvedValueOnce({ action: 'resume', target: { id: 'c_1' } })
    await wrapper.find('button').trigger('click')
    expect(wrapper.find('button').attributes('data-loading')).toBe('true')
    await flushPromises()
    expect(wrapper.text()).toBe('Continue reading')
})

it('has nothing to start in a series without readable volumes', async () => {
    vi.mocked(apiFetch).mockResolvedValueOnce({ reason: 'empty', series_id: 's_1' })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toBe('No readable volumes')
    expect(wrapper.find('button').attributes('disabled')).toBeDefined()
})

it("finds a series' first volume before clearing it, retrying a failed lookup", async () => {
    vi.mocked(apiFetch).mockResolvedValueOnce({ reason: 'caught_up', series_id: 's_1' })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toBe('Read again')

    vi.mocked(apiFetch).mockRejectedValueOnce(new Error('down'))
    await wrapper.find('button').trigger('click')
    await flushPromises()
    expect(showClearReadingModal).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Retry')

    vi.mocked(apiFetch).mockResolvedValueOnce({ ids: ['c_1'] })
    await wrapper.find('button').trigger('click')
    await flushPromises()
    expect(showClearReadingModal).toHaveBeenCalledTimes(1)
    expect(router.push).toHaveBeenCalledWith({ path: '/r/c_1', query: { page: 'resume' } })
    expect(wrapper.text()).toBe('Read again')
})

describe('Read again across content', () => {
    // Both series are caught up; the first-volume lookup is the test's to settle.
    function serve(lookup: Promise<unknown>) {
        vi.mocked(apiFetch).mockImplementation(async (path: string) =>
            path.startsWith('/content/ids')
                ? lookup
                : { reason: 'caught_up', series_id: path.split('/')[2] }
        )
    }

    it('drops a lookup that settles after the page moved on', async () => {
        const lookup = Promise.withResolvers<unknown>()
        serve(lookup.promise)
        const wrapper = render()
        await flushPromises()
        await wrapper.find('button').trigger('click')
        await wrapper.setProps({ contentId: 's_2' })
        lookup.resolve({ ids: ['c_1'] })
        await flushPromises()
        expect(showClearReadingModal).not.toHaveBeenCalled()
        expect(router.push).not.toHaveBeenCalled()
    })

    it("doesn't carry a failed lookup over to other content", async () => {
        const down = Promise.reject(new Error('down'))
        down.catch(() => {})
        serve(down)
        const wrapper = render()
        await flushPromises()
        await wrapper.find('button').trigger('click')
        await flushPromises()
        expect(wrapper.text()).toContain('Retry')
        await wrapper.setProps({ contentId: 's_2' })
        await flushPromises()
        expect(wrapper.text()).toBe('Read again')
    })
})
