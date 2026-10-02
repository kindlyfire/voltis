import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, ref, type Ref } from 'vue'
import { apiFetch } from '@/utils/fetch'
import { useSiblings, type Siblings } from './useSiblings'

vi.mock('@/utils/fetch', async importOriginal => ({
    ...(await importOriginal<typeof import('@/utils/fetch')>()),
    apiFetch: vi.fn(),
}))

type Placed = { id: string; parent_id: string | null }
// The server: each content's series, and each series' volumes.
let parents: Record<string, string | null>
let lists: Record<string, string[]>
let failLists: number

beforeEach(() => {
    parents = {}
    lists = {}
    failLists = 0
    vi.mocked(apiFetch).mockReset()
    vi.mocked(apiFetch).mockImplementation(async (path: string) => {
        const url = new URL(path, 'http://x')
        if (url.pathname === '/content') {
            if (failLists > 0 && failLists--) throw new Error('down')
            const ids = lists[url.searchParams.get('parent_id')!] ?? []
            return { data: ids.map(id => ({ id })), total: ids.length }
        }
        const id = url.pathname.split('/')[2]!
        return { id, parent_id: parents[id] ?? null }
    })
})

function render(content: Ref<Placed | null>) {
    let siblings!: Ref<Siblings>
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    mount(
        defineComponent({
            setup() {
                siblings = useSiblings(content)
                return () => null
            },
        }),
        { global: { plugins: [[VueQueryPlugin, { queryClient }]] } }
    )
    return siblings
}

describe('useSiblings', () => {
    it('is ready for standalone content without asking', () => {
        const siblings = render(ref({ id: 'c_1', parent_id: null }))
        expect(siblings.value).toMatchObject({ status: 'ready', items: [], next: null })
        expect(apiFetch).not.toHaveBeenCalled()
    })

    it('loads only while the content or its list is to come', async () => {
        const content = ref<Placed | null>(null)
        const siblings = render(content)
        expect(siblings.value.status).toBe('loading')
        lists.s_1 = ['c_1', 'c_2']
        content.value = { id: 'c_1', parent_id: 's_1' }
        await Promise.resolve()
        expect(siblings.value.status).toBe('loading')
        await flushPromises()
        expect(siblings.value).toMatchObject({ status: 'ready', index: 0, next: { id: 'c_2' } })
    })

    it.each([
        ['empty', []],
        ['without the current volume', ['c_8', 'c_9']],
    ])('is an error once a list comes %s', async (_name, ids) => {
        lists.s_1 = ids
        const siblings = render(ref({ id: 'c_1', parent_id: 's_1' }))
        await flushPromises()
        expect(siblings.value).toMatchObject({ status: 'error', items: [], next: null })
    })

    it('retries by reading the content again, following it to its new series', async () => {
        lists.s_1 = ['c_9']
        const siblings = render(ref({ id: 'c_1', parent_id: 's_1' }))
        await flushPromises()
        expect(siblings.value.status).toBe('error')

        parents.c_1 = 's_2'
        lists.s_2 = ['c_0', 'c_1', 'c_2']
        siblings.value.retry()
        expect(siblings.value.status).toBe('loading')
        await flushPromises()
        expect(siblings.value).toMatchObject({ status: 'ready', index: 1, next: { id: 'c_2' } })
    })

    it('is an error when the list fails, until a retry gets it', async () => {
        failLists = 1
        lists.s_1 = ['c_1', 'c_2']
        parents.c_1 = 's_1'
        const siblings = render(ref({ id: 'c_1', parent_id: 's_1' }))
        await flushPromises()
        expect(siblings.value.status).toBe('error')
        siblings.value.retry()
        await flushPromises()
        expect(siblings.value).toMatchObject({ status: 'ready', next: { id: 'c_2' } })
    })
})
