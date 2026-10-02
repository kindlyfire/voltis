import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createHead } from '@unhead/vue/client'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import BrokenRefMerge from './BrokenRefMerge.vue'
import BrokenRefsPage from './BrokenRefsPage.vue'

/** The server: the library's broken refs, the URIs with user data, and the repairs posted. */
let refs: Array<{ id: string; uri: string }>
let userUris: string[]
let failingPreviews: Set<string>
const posted: unknown[] = []

function respond(url: string, init?: RequestInit): unknown {
    const path = url.replace(/^.*\/api/, '')
    if (path === '/libraries') return [{ id: 'l_1', name: 'Lib' }]
    if (path === '/content/broken-refs') return [{ library_id: 'l_1', count: refs.length }]
    if (path.startsWith('/content/broken-refs/l_1') && init?.method === 'POST') {
        const body = JSON.parse(init.body as string)
        posted.push(body)
        for (const [id, uri] of Object.entries(body.update as Record<string, string>)) {
            refs = refs.filter(r => r.id !== id)
            if (!userUris.includes(uri)) userUris.push(uri)
        }
        return {}
    }
    if (path.startsWith('/content/broken-refs/l_1')) {
        const data = refs.map(r => ({ ...r, status: 'reading', progress: {}, last_read_at: null }))
        return { data, total: data.length }
    }
    if (path === '/content/refs/l_1') return { content_uris: ['a', 'b', 'c'], user_uris: userUris }
    const uri = new URL(url, 'http://x').searchParams.get('uri')!
    if (failingPreviews.has(uri)) throw new Error('500')
    return { status: 'completed', progress: {}, last_read_at: null }
}

let queryClient: QueryClient
const Combobox = defineComponent({
    name: 'ACombobox',
    props: { label: String, modelValue: String },
    emits: ['update:model-value'],
    template: '<i />',
})

beforeEach(() => {
    refs = [
        { id: 'r_1', uri: 'old/1' },
        { id: 'r_2', uri: 'old/2' },
    ]
    userUris = ['a', 'b']
    failingPreviews = new Set()
    posted.length = 0
    vi.stubGlobal(
        'fetch',
        vi.fn(async (url: string, init?: RequestInit) => {
            try {
                return Response.json(respond(url, init))
            } catch {
                return new Response('{}', { status: 500 })
            }
        })
    )
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})

afterEach(() => vi.unstubAllGlobals())

async function render() {
    const wrapper = mount(BrokenRefsPage, {
        global: {
            plugins: [[VueQueryPlugin, { queryClient }], createHead()],
            stubs: {
                AIconButton: { template: '<span />' },
                AMenu: { template: '<div />' },
                ACombobox: Combobox,
                // Reka's select fails to re-render in jsdom.
                ASelect: { template: '<i />' },
            },
        },
    })
    await flushPromises()
    const setTarget = async (row: number, uri: string) => {
        const boxes = wrapper.findAllComponents(Combobox)
        boxes
            .filter(c => c.props('label')?.startsWith('New ref'))
            [row]!.vm.$emit('update:model-value', uri)
        await flushPromises()
    }
    const save = async () => {
        await wrapper
            .findAll('button')
            .find(b => b.text() === 'Save')!
            .trigger('click')
        await flushPromises()
    }
    return { wrapper, setTarget, save }
}

it('drops a side chosen for one destination when the destination changes', async () => {
    failingPreviews.add('b')
    const { wrapper, setTarget, save } = await render()
    await setTarget(0, 'a')
    wrapper.findComponent(BrokenRefMerge).vm.$emit('update:keep', 'source')
    await setTarget(0, 'b')
    expect(wrapper.text()).toContain("Couldn't load it.")
    await save()
    expect(posted).toEqual([{ delete: [], update: { r_1: 'b' }, keep: {} }])
})

it('previews a second repair onto a destination the first one filled', async () => {
    const { wrapper, setTarget, save } = await render()
    await setTarget(0, 'c')
    expect(wrapper.findComponent(BrokenRefMerge).exists()).toBe(false)
    await save()
    await setTarget(0, 'c')
    expect(wrapper.findComponent(BrokenRefMerge).exists()).toBe(true)
})
