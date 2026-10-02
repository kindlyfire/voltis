import { QueryObserver, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { libraryScope } from '@/utils/api/catalog'
import {
    bumpContinueReading,
    invalidateContinueReading,
    invalidateReading,
    readingApi,
} from '@/utils/api/reading'
import type { ContinueEntry, Content } from '@/utils/api/types'
import { queryClient } from '@/utils/misc'

const key = ['content', 'list', libraryScope(undefined), 'continue-reading', 10]

afterEach(() => {
    queryClient.clear()
    vi.unstubAllGlobals()
})

describe('invalidateContinueReading', () => {
    it('refetches rather than joining an initial fetch that predates the write', async () => {
        let resolveStale!: (v: string) => void
        const responses = [new Promise<string>(r => (resolveStale = r)), Promise.resolve('fresh')]
        const observer = new QueryObserver(queryClient, {
            queryKey: key,
            queryFn: () => responses.shift()!,
        })
        const unsubscribe = observer.subscribe(() => {})

        const invalidated = invalidateContinueReading()
        resolveStale('stale')
        await invalidated

        expect(observer.getCurrentResult().data).toBe('fresh')
        unsubscribe()
    })
})

describe.each([
    ['seriesReading', () => readingApi.seriesReading('s_1', { action: 'clear' })],
    [
        'a page command',
        () => readingApi.post('s_1', { op: 'clear' }).then(() => invalidateReading('s_1', null)),
    ],
])('after %s', (_name, write) => {
    it('refetches past an initial fetch that predates the write', async () => {
        vi.stubGlobal(
            'fetch',
            vi.fn(async () => Response.json({ count: 1 }))
        )
        let resolveStale!: (v: string) => void
        const responses = [new Promise<string>(r => (resolveStale = r)), Promise.resolve('fresh')]
        const observer = new QueryObserver(queryClient, {
            queryKey: ['content', 's_1', 'continue'],
            queryFn: () => responses.shift()!,
        })
        const unsubscribe = observer.subscribe(() => {})
        const written = write()
        await flushPromises()
        resolveStale('stale')
        await written
        await flushPromises()
        expect(observer.getCurrentResult().data).toBe('fresh')
        unsubscribe()
    })
})

describe('useRecentlyUpdated', () => {
    it("is the grid's sort, without the count query", async () => {
        const fetch = vi.fn(async (_input: RequestInfo) => Response.json({ data: [] }))
        vi.stubGlobal('fetch', fetch)
        const Consumer = { setup: () => void readingApi.useRecentlyUpdated(), template: '<i />' }
        const wrapper = mount(Consumer, {
            global: { plugins: [[VueQueryPlugin, { queryClient }]] },
        })
        await flushPromises()
        expect(String(fetch.mock.calls[0]![0])).toContain('count=false')
        wrapper.unmount()
    })
})

describe('bumpContinueReading', () => {
    const content = (id: string, parent_id: string | null = null) => ({ id, parent_id }) as Content
    const entry = (item: Content, series: Content | null) =>
        ({ item, series, action: 'resume', is_new: false }) as ContinueEntry
    const entries = [
        entry(content('a1', 'a'), content('a')),
        entry(content('b2', 'b'), content('b')),
        entry(content('solo'), null),
    ]

    it.each([
        ['moves a series entry to the front', content('b1', 'b'), ['b', 'a', 'solo']],
        ['moves a standalone entry to the front', content('solo'), ['solo', 'a', 'b']],
        ['leaves the list unchanged without a match', content('c1', 'c'), ['a', 'b', 'solo']],
    ])('%s', (_, item, order) => {
        queryClient.setQueryData(key, entries)
        bumpContinueReading(item)
        const result = queryClient.getQueryData<ContinueEntry[]>(key)!
        expect(result.map(e => e.series?.id ?? e.item.id)).toEqual(order)
    })
})
