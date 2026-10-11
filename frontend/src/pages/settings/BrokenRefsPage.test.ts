import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createHead } from '@unhead/vue/client'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import BrokenRefsPage from './BrokenRefsPage.vue'

const { showToast } = vi.hoisted(() => ({ showToast: vi.fn() }))
vi.mock('@/ui/useToast', () => ({ useToast: () => ({ show: showToast }) }))

const row = (id: string, uri: string) => ({
    id,
    uri,
    library_id: 'l_1',
    status: 'reading',
    progress: {},
    last_read_at: null,
})

const CHANGED = 'Broken references changed. Check the preview and save again.'

/** The server: the library's broken groups, its previews, and the saves posted. */
let groups: unknown[]
let previewFails: number
let saveStatus: number
let brokenRefsFetches: number
const posted: unknown[] = []

function respond(url: string, init?: RequestInit): Response {
    const path = url.replace(/^.*\/api/, '')
    const post = init?.method === 'POST'
    if (path === '/libraries') return Response.json([{ id: 'l_1', name: 'Lib' }])
    if (path === '/content/broken-refs') {
        return Response.json([{ library_id: 'l_1', count: groups.length }])
    }
    if (path === '/content/broken-refs/l_1/preview') {
        if (previewFails-- > 0) return Response.json({}, { status: 500 })
        const { overrides } = JSON.parse(init!.body as string)
        const to = Object.entries<string | null>({
            'comic/Old/ch1': 'comic/New/ch1',
            'comic/Old/ch2': 'comic/New/v1_ch2',
            ...overrides,
        })
        return Response.json({
            pairs: to.flatMap(([from, dst]) =>
                dst ? [{ from, to: dst, by: 'exact', existing: null, list_merges: 0 }] : []
            ),
            leftovers: to.flatMap(([from, dst]) => (dst ? [] : [from])),
            free_targets: [],
        })
    }
    if (path === '/content/broken-refs/l_1' && post) {
        const body = JSON.parse(init.body as string)
        posted.push(body)
        if (saveStatus === 409) return Response.json({ error: CHANGED }, { status: 409 })
        groups = []
        return Response.json({
            moved: body.moves.reduce(
                (n: number, m: { pairs: object }) => n + Object.keys(m.pairs).length,
                0
            ),
            merged: 0,
            deleted: body.delete.refs.length + body.delete.list_entries.length,
        })
    }
    if (path.startsWith('/content/broken-refs/l_1')) {
        brokenRefsFetches++
        return Response.json({ data: groups, total: groups.length })
    }
    if (path === '/content/refs/l_1') {
        return Response.json({ content_uris: ['comic/New', 'comic/New/ch1', 'comic/New/v1_ch2'] })
    }
    return Response.json({}, { status: 404 })
}

let queryClient: QueryClient
const Combobox = defineComponent({
    name: 'ACombobox',
    props: { label: String, modelValue: String },
    emits: ['update:model-value'],
    template: '<i />',
})

beforeEach(() => {
    groups = [
        {
            uri: 'comic/Old',
            items: [row('r_1', 'comic/Old/ch1'), row('r_2', 'comic/Old/ch2')],
            list_entries: [],
            suggestions: [{ uri: 'comic/New', matched: 2, total: 2, same: false }],
        },
        {
            uri: 'book/Gone',
            items: [row('r_3', 'book/Gone')],
            list_entries: [
                { id: 'e_1', list_id: 'cl_1', list_name: 'Faves', uri: 'book/Gone', notes: null },
            ],
            suggestions: [],
        },
    ]
    previewFails = 0
    saveStatus = 200
    brokenRefsFetches = 0
    posted.length = 0
    showToast.mockClear()
    vi.stubGlobal(
        'fetch',
        vi.fn(async (url: string, init?: RequestInit) => respond(url, init))
    )
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
})

afterEach(() => vi.unstubAllGlobals())

async function render() {
    const wrapper = mount(BrokenRefsPage, {
        global: {
            plugins: [[VueQueryPlugin, { queryClient }], createHead()],
            stubs: {
                // Its tooltip needs a provider.
                AIconButton: { props: ['label'], template: '<button :aria-label="label" />' },
                AMenu: { template: '<div />' },
                ACombobox: Combobox,
                // Reka's select fails to re-render in jsdom.
                ASelect: { template: '<i />' },
            },
        },
    })
    await flushPromises()
    const button = (name: string) => wrapper.findAll('button').find(b => b.text() === name)!
    const saveDisabled = () => button('Save').attributes('aria-disabled') === 'true'
    const box = (label: string) =>
        wrapper.findAllComponents(Combobox).find(c => c.props('label') === label)!
    const choose = async (label: string, uri: string) => {
        box(label).vm.$emit('update:model-value', uri)
        await flushPromises()
    }
    const save = async () => {
        await button('Save').trigger('click')
        await flushPromises()
    }
    return { wrapper, button, box, choose, saveDisabled, save }
}

it('moves a group with the previewed pairs and refreshes lists', async () => {
    const listsKey = ['custom-lists', 'list', 'all']
    queryClient.setQueryData(listsKey, [])
    const { wrapper, button, choose, saveDisabled, save } = await render()
    expect(saveDisabled()).toBe(true)
    await choose('Move comic/Old to', 'comic/New')
    expect(wrapper.text()).toContain('2 of 2 items match')
    expect(saveDisabled()).toBe(false)
    await button('Details').trigger('click')
    await choose('Destination for comic/Old/ch2', 'comic/New/ch2b')
    await save()
    expect(posted).toEqual([
        {
            moves: [
                {
                    keep: 'newer',
                    pairs: {
                        'comic/Old/ch1': 'comic/New/ch1',
                        'comic/Old/ch2': 'comic/New/ch2b',
                    },
                },
            ],
            delete: { refs: [], list_entries: [] },
        },
    ])
    expect(queryClient.getQueryState(listsKey)?.isInvalidated).toBe(true)
    expect(showToast).toHaveBeenCalledWith({ message: 'Moved 2 items' })
})

it('keeps Save disabled until the preview loads, and keeps edits on a 409', async () => {
    previewFails = 1
    saveStatus = 409
    const { wrapper, button, box, choose, saveDisabled, save } = await render()
    await choose('Move comic/Old to', 'comic/New')
    expect(wrapper.text()).toContain("Couldn't load the preview.")
    expect(saveDisabled()).toBe(true)

    await button('Retry').trigger('click')
    await flushPromises()
    expect(saveDisabled()).toBe(false)

    const fetches = brokenRefsFetches
    await save()
    expect(wrapper.text()).toContain(CHANGED)
    expect(box('Move comic/Old to').props('modelValue')).toBe('comic/New')
    expect(brokenRefsFetches).toBeGreaterThan(fetches)
})

it('deletes exactly the shown rows and entries', async () => {
    const { wrapper, save } = await render()
    await wrapper.find('button[aria-label="Delete the data for book/Gone"]').trigger('click')
    await save()
    expect(posted).toEqual([{ moves: [], delete: { refs: ['r_3'], list_entries: ['e_1'] } }])
    expect(showToast).toHaveBeenCalledWith({ message: 'Deleted 2 items' })
})
