import { QueryObserver, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { libraryScope } from '@/utils/api/catalog'
import {
    bumpRecentlyRead,
    contentApi,
    invalidateRecentlyRead,
    trackRecentlyReadWrite,
} from '@/utils/api/content'
import type { Content, RecentlyReadEntry } from '@/utils/api/types'
import { queryClient } from '@/utils/misc'

const key = ['content', 'list', libraryScope(undefined), 'recently-read', 10]

afterEach(() => {
    queryClient.clear()
    vi.unstubAllGlobals()
})

describe('invalidateRecentlyRead', () => {
    it('refetches rather than joining an initial fetch that predates the write', async () => {
        let resolveStale!: (v: string) => void
        const responses = [new Promise<string>(r => (resolveStale = r)), Promise.resolve('fresh')]
        const observer = new QueryObserver(queryClient, {
            queryKey: key,
            queryFn: () => responses.shift()!,
        })
        const unsubscribe = observer.subscribe(() => {})

        const invalidated = invalidateRecentlyRead()
        resolveStale('stale')
        await invalidated

        expect(observer.getCurrentResult().data).toBe('fresh')
        unsubscribe()
    })
})

describe('useRecentlyRead', () => {
    it.each([true, false])('waits for a tracked write (succeeds: %s)', async succeeds => {
        const fetch = vi.fn(async () => Response.json([]))
        vi.stubGlobal('fetch', fetch)
        const write = Promise.withResolvers<void>()
        trackRecentlyReadWrite(write.promise)
        const Consumer = { setup: () => void contentApi.useRecentlyRead(), template: '<i />' }
        const wrapper = mount(Consumer, {
            global: { plugins: [[VueQueryPlugin, { queryClient }]] },
        })

        await flushPromises()
        expect(fetch).not.toHaveBeenCalled()
        if (succeeds) write.resolve()
        else write.reject(new Error('offline'))
        await flushPromises()
        expect(fetch).toHaveBeenCalledTimes(1)
        wrapper.unmount()
    })
})

describe('bumpRecentlyRead', () => {
    const content = (id: string, parent_id: string | null = null) => ({ id, parent_id }) as Content
    const entries: RecentlyReadEntry[] = [
        { item: content('a1', 'a'), series: content('a') },
        { item: content('b2', 'b'), series: content('b') },
        { item: content('solo'), series: null },
    ]

    it.each([
        ['moves a series entry to the front', content('b1', 'b'), ['b', 'a', 'solo']],
        ['moves a standalone entry to the front', content('solo'), ['solo', 'a', 'b']],
        ['leaves the list unchanged without a match', content('c1', 'c'), ['a', 'b', 'solo']],
    ])('%s', (_, item, order) => {
        queryClient.setQueryData(key, entries)
        bumpRecentlyRead(item)
        const result = queryClient.getQueryData<RecentlyReadEntry[]>(key)!
        expect(result.map(e => e.series?.id ?? e.item.id)).toEqual(order)
    })
})
