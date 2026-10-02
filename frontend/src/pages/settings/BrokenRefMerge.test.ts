import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import { contentApi } from '@/utils/api/content'
import type { BrokenUserToContent } from '@/utils/api/types'
import BrokenRefMerge from './BrokenRefMerge.vue'

vi.mock('@/utils/api/content', () => ({ contentApi: { userDataAt: vi.fn() } }))

it('shows a failed preview as such, and allows a choice only once it loads', async () => {
    vi.mocked(contentApi.userDataAt).mockRejectedValueOnce(new Error('500'))
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = mount(BrokenRefMerge, {
        props: {
            source: {
                status: 'reading',
                progress: {},
                last_read_at: null,
            } as unknown as BrokenUserToContent,
            libraryId: 'l_1',
            uri: 'comic/a',
            keep: 'newer',
        },
        global: { plugins: [[VueQueryPlugin, { queryClient }]] },
    })
    await flushPromises()
    expect(wrapper.text()).toContain("Couldn't load it.")
    const segments = () =>
        wrapper.findAll('[role="radio"], button').filter(b => b.text().startsWith('Keep'))
    expect(segments()).toHaveLength(3)
    expect(
        segments().every(
            b => b.attributes('disabled') !== undefined || b.attributes('aria-disabled') === 'true'
        )
    ).toBe(true)

    vi.mocked(contentApi.userDataAt).mockResolvedValueOnce(null)
    await wrapper
        .findAll('button')
        .find(b => b.text() === 'Retry')!
        .trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Existing:—')
    expect(
        segments().some(
            b => b.attributes('disabled') === undefined && b.attributes('aria-disabled') !== 'true'
        )
    ).toBe(true)
})
