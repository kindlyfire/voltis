import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { shallowMount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { MetadataLink } from '@/utils/api/metadata'
import type { Content } from '@/utils/api/types'
import { showProviderSearchModal } from './components/ProviderSearchModal.vue'
import InfoHeader from './InfoHeader.vue'

vi.mock('./components/ProviderSearchModal.vue', () => ({ showProviderSearchModal: vi.fn() }))

const content = { id: 'c_1', type: 'comic_series', title: 'Local', meta: {} } as unknown as Content

const link = (over: Partial<MetadataLink>): MetadataLink =>
    ({ provider: 'mangabaka', label: 'MangaBaka', candidates: [], ...over }) as MetadataLink

function mountWith(links: MetadataLink[]) {
    const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } })
    queryClient.setQueryData(['users', 'me'], { permissions: ['ADMIN'] })
    queryClient.setQueryData(['content', 'metadata', 'c_1'], { links, layers: [] })
    return shallowMount(InfoHeader, {
        props: { content },
        global: { plugins: [[VueQueryPlugin, { queryClient }]], stubs: { RouterLink: true } },
    })
}

describe('InfoHeader', () => {
    it('offers auto links and pending reviews for checking', async () => {
        const review = link({ state: 'review', candidates: [{}, {}] as MetadataLink['candidates'] })
        const w = mountWith([link({ state: 'linked', origin: 'auto' }), review])
        expect(w.text()).toContain('Matched automatically on MangaBaka · Wrong match?')
        expect(w.text()).toContain('2 possible matches on MangaBaka · Review')
        const button = w.findAll('button').find(b => b.text() === 'Review')!
        await button.trigger('click')
        expect(showProviderSearchModal).toHaveBeenCalledWith('c_1', review, expect.any(String))
    })

    it('says nothing for manual links', () => {
        expect(mountWith([link({ state: 'linked', origin: 'manual' })]).text()).not.toContain(
            'MangaBaka'
        )
    })
})
