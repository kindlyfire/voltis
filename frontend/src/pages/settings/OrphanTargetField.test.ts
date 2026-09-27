import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { apiFetch } from '@/utils/fetch'
import { addOverlays, settle } from '@/utils/modalTesting'
import OrphanTargetField from './OrphanTargetField.vue'

vi.mock('@/utils/fetch', async importOriginal => ({
    ...(await importOriginal<typeof import('@/utils/fetch')>()),
    apiFetch: vi.fn(),
}))

let queries: URLSearchParams[]
let wrapper: ReturnType<typeof mount>

beforeEach(() => {
    vi.useFakeTimers()
    // Not implemented by jsdom.
    Element.prototype.scrollIntoView = () => {}
    addOverlays()
    queries = []
    vi.mocked(apiFetch).mockImplementation(async url => {
        const [path, query] = url.split('?')
        if (path !== '/content/orphaned-metadata/l1/targets') throw new Error(`unexpected ${url}`)
        queries.push(new URLSearchParams(query))
        return { data: [{ uri: 'comic/Found', title: 'Found Title' }] }
    })
    wrapper = mount(OrphanTargetField, {
        attachTo: document.body,
        props: {
            libraryId: 'l1',
            series: true,
            label: 'Move comic/Gone to',
            modelValue: 'comic/Chosen',
        },
        global: {
            plugins: [[VueQueryPlugin, { queryClient: new QueryClient() }]],
            stubs: { ATooltip: { template: '<slot />' } },
        },
    })
})

afterEach(() => {
    wrapper.unmount()
    vi.useRealTimers()
    document.body.innerHTML = ''
})

describe('OrphanTargetField', () => {
    it('searches compatible content on the server, keeping the chosen one listed', async () => {
        await settle()
        expect(queries.map(q => Object.fromEntries(q))).toEqual([
            { q: '', series: 'true', limit: '50' },
        ])

        await wrapper.find('input').setValue('foun')
        await settle()
        expect(Object.fromEntries(queries.at(-1)!)).toEqual({
            q: 'foun',
            series: 'true',
            limit: '50',
        })
        const listed = document.getElementById('overlays')!.textContent
        expect(listed).toContain('comic/Chosen')
        expect(listed).toContain('comic/Found · Found Title')
    })
})
