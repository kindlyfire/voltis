import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { Content } from '@/utils/api/types'
import ContentInfo from './ContentInfo.vue'
import DetailValue from './DetailValue.vue'
import { useContentDetails } from './useContentDetails'

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

function mountInfo(over: Partial<Content>) {
    const queryClient = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } })
    queryClient.setQueryData(['users', 'me'], { permissions: [] })
    const component = { template: '<div />' }
    const router = createRouter({
        history: createMemoryHistory(),
        routes: [
            { path: '/', component },
            { path: '/:kind/:key', name: 'facet', component },
        ],
    })
    const content = { id: 'c_1', type: 'book', title: 'Local', meta, ...over } as Content
    const Harness = defineComponent(() => {
        const details = useContentDetails(() => content)
        return () => h(ContentInfo, { details: details.value })
    })
    return mount(Harness, { global: { plugins: [[VueQueryPlugin, { queryClient }], router] } })
}
const links = (w: ReturnType<typeof mountInfo>) =>
    w.findAll('a').map(a => [a.text(), a.attributes('href')])

describe('ContentInfo', () => {
    it('links values with a key on a top-level entry', () => {
        const w = mountInfo({ facet_keys } as Partial<Content>)
        expect(links(w)).toEqual([
            ['Dana Kell', '/people/dana-kell'],
            ['Brightwell Press', '/publishers/brightwell-press'],
            ['Sci fi', '/genres/sci-fi'],
            ['Time Loops', '/tags/time-loops'],
        ])
        expect(w.text()).toContain('Dana Kell (writer), ??? (inker)')
    })

    it('renders plain text without keys, as on a child', () => {
        const w = mountInfo({})
        expect(links(w)).toEqual([])
        expect(w.text()).toContain('Sci fi')
        expect(w.text()).toContain('Time Loops')
    })
})

describe('DetailValue', () => {
    it('cuts a long list to the limit with a button for the rest', () => {
        const items = ['a', 'b', 'c', 'd', 'e'].map(text => ({ text }))
        const w = mount(DetailValue, {
            props: { row: { key: 'genres', label: 'Genres', items }, limit: 3 },
        })
        expect(w.text().split('and 2')[0]).toBe('a, b, c')
        const more = w.get('button')
        expect(more.text()).toBe('and 2 more genres')
    })
})
