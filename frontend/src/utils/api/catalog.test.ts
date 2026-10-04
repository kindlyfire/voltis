import { QueryClient } from '@tanstack/vue-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { libraryScope, refetchCatalog, syncCatalog } from '@/utils/api/catalog'
import { ws } from '@/utils/ws'

vi.mock('@/utils/ws', () => {
    const handlers = new Map<string, Set<(msg: any) => void>>()
    return {
        ws: {
            on(type: string, handler: (msg: any) => void) {
                let set = handlers.get(type)
                if (!set) handlers.set(type, (set = new Set()))
                set.add(handler)
                return () => set!.delete(handler)
            },
            emit(type: string, msg: any) {
                for (const handler of [...(handlers.get(type) ?? [])]) handler(msg)
            },
        },
    }
})

const emit = (ws as unknown as { emit: (type: string, msg: any) => void }).emit

const keys = [
    ['libraries'],
    ['content', 'c_1'],
    ['content', 'list', libraryScope('l1'), { library_id: 'l1' }],
    ['content', 'list', libraryScope('l2'), { library_id: 'l2' }],
    ['content', 'broken-refs', libraryScope('l2'), {}],
    ['metadata', 'review', libraryScope('l2'), { libraryId: 'l2' }],
    ['metadata', 'summary'],
    ['metadata-config'],
    ['content', 'facets', libraryScope(null), 'tags', 'list', { library_id: 'l1' }],
]

function seed(): QueryClient {
    const client = new QueryClient()
    for (const key of keys) client.setQueryData(key, {})
    return client
}

afterEach(() => {
    vi.useRealTimers()
})

describe('syncCatalog', () => {
    it("refetches the changed library's queries and those of none, throttled to 500ms", async () => {
        vi.useFakeTimers()
        const client = seed()
        syncCatalog(client)
        const invalidated = () => keys.filter(key => client.getQueryState(key)?.isInvalidated)

        emit('catalog_changed', { library_id: 'l1' })
        expect(invalidated()).toEqual([keys[0], keys[1], keys[2], keys[6], keys[8]])

        emit('catalog_changed', { library_id: 'l2' })
        expect(invalidated()).toHaveLength(5)
        await vi.advanceTimersByTimeAsync(500)
        expect(invalidated()).toHaveLength(8)

        emit('$open', {})
        await vi.advanceTimersByTimeAsync(2000)
        expect(invalidated()).toHaveLength(8)
    })
})

describe('refetchCatalog', () => {
    it('refetches every catalog query but the one set', async () => {
        const client = seed()
        await refetchCatalog(client, keys[1])
        expect(keys.filter(key => client.getQueryState(key)?.isInvalidated)).toEqual([
            ...keys.slice(2, 7),
            keys[8],
        ])
    })
})
