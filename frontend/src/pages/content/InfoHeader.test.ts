import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { shallowMount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
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
        global: {
            plugins: [[VueQueryPlugin, { queryClient }]],
            stubs: {
                RouterLink: { template: '<a><slot /></a>' },
                AChip: { template: '<span><slot /></span>' },
            },
        },
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

describe('InfoHeader facet links', () => {
    const meta = {
        staff: [
            { name: 'Dana Kell', role: 'writer' },
            { name: '???', role: 'inker' },
        ],
        publishers: ['Brightwell Press'],
        genres: ['sci_fi'],
        tags: ['Time Loops'],
    }
    const facet_keys = {
        staff: ['dana kell', null],
        publishers: ['brightwell press'],
        genres: ['sci fi'],
        tags: ['time loops'],
    }

    function mountLinks(over: Partial<Content>) {
        const queryClient = new QueryClient({
            defaultOptions: { queries: { staleTime: Infinity } },
        })
        queryClient.setQueryData(['users', 'me'], { permissions: [] })
        const component = { template: '<div />' }
        const router = createRouter({
            history: createMemoryHistory(),
            routes: [
                { path: '/', component },
                { path: '/:kind/:key', name: 'facet', component },
            ],
        })
        return shallowMount(InfoHeader, {
            props: { content: { ...content, type: 'book', meta, ...over } },
            global: {
                plugins: [[VueQueryPlugin, { queryClient }], router],
                stubs: { RouterLink: false },
            },
        })
    }
    const links = (w: ReturnType<typeof mountLinks>) =>
        w.findAll('dl a').map(a => [a.text(), a.attributes('href')])

    it('links values with a key on a top-level entry', () => {
        const w = mountLinks({ facet_keys } as Partial<Content>)
        expect(links(w)).toEqual([
            ['Dana Kell', '/people/dana-kell'],
            ['Brightwell Press', '/publishers/brightwell-press'],
            ['Sci fi', '/genres/sci-fi'],
            ['Time Loops', '/tags/time-loops'],
        ])
        expect(w.find('dl').text()).toContain('Dana Kell (writer), ??? (inker)')
    })

    it('renders plain text without keys, as on a child', () => {
        const w = mountLinks({})
        expect(links(w)).toEqual([])
        expect(w.find('dl').text()).toContain('Sci fi')
        expect(w.find('dl').text()).toContain('Time Loops')
    })
})
