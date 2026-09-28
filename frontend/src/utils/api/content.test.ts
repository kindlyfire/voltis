import { QueryObserver } from '@tanstack/vue-query'
import { afterEach, describe, expect, it } from 'vitest'
import { libraryScope } from '@/utils/api/catalog'
import { invalidateRecentlyRead } from '@/utils/api/content'
import { queryClient } from '@/utils/misc'

afterEach(() => queryClient.clear())

describe('invalidateRecentlyRead', () => {
    it('refetches rather than joining an initial fetch that predates the write', async () => {
        let resolveStale!: (v: string) => void
        const responses = [new Promise<string>(r => (resolveStale = r)), Promise.resolve('fresh')]
        const observer = new QueryObserver(queryClient, {
            queryKey: ['content', 'list', libraryScope(undefined), 'recently-read', 10],
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
