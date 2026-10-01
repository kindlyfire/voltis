import { afterEach, expect, it, vi } from 'vitest'
import { contentApi, invalidateStatusChange } from '@/utils/api/content'
import type { Content, UserToContent } from '@/utils/api/types'
import { createComicState } from './createComicState'

vi.mock('@/utils/api/content', () => ({
    contentApi: { get: vi.fn(), updateUserData: vi.fn() },
    bumpRecentlyRead: vi.fn(),
    invalidateRecentlyRead: vi.fn(),
    invalidateStatusChange: vi.fn(),
}))

vi.mock('./usePageLoader', () => ({
    createPageLoader: (index: number) => ({
        index,
        blobUrl: null,
        load: vi.fn(),
        dispose: vi.fn(),
    }),
    getPagesInPreloadOrder: () => [],
}))

afterEach(() => {
    vi.useRealTimers()
})

it('sets a status only after the reader changes page', async () => {
    vi.useFakeTimers()
    vi.mocked(contentApi.get).mockResolvedValue({
        id: 'c_1',
        parent_id: 's_1',
        file_data: { pages: [0, 1, 2].map(i => [`${i}.png`, 800, 1200]) },
        user_data: null,
    } as unknown as Content)
    vi.mocked(contentApi.updateUserData).mockImplementation(
        async (_id, update) =>
            ({ status: update.status ?? null, progress: update.progress }) as UserToContent
    )
    const writes = () => vi.mocked(contentApi.updateUserData).mock.calls.map(([, update]) => update)

    const comic = createComicState('c_1', 'resume')
    comic.setHandlers({ onReady: vi.fn() })
    await vi.advanceTimersByTimeAsync(1000)
    expect(writes()).toHaveLength(1)
    expect(writes()[0]!.status).toBeUndefined()
    expect(writes()[0]!.progress?.current_page).toBe(0)

    comic.setPage(1)
    await vi.advanceTimersByTimeAsync(1000)
    expect(writes()[1]!.status).toBe('reading')

    comic.setPage(2)
    await comic.leave()
    await comic.dispose()
    expect(writes()[2]!.status).toBe('completed')
    expect(vi.mocked(invalidateStatusChange).mock.calls).toEqual([['s_1'], ['s_1']])
})

it('requests page sizes and maps unsized pages to 0, 0', async () => {
    vi.mocked(contentApi.get).mockResolvedValue({
        id: 'c_1',
        file_data: { pages: [['0.png', 800, 1200], ['1.png']] },
        user_data: null,
    } as unknown as Content)

    const comic = createComicState('c_1', 0)
    const onReady = vi.fn()
    comic.setHandlers({ onReady })
    await vi.waitFor(() => expect(onReady).toHaveBeenCalled())
    expect(vi.mocked(contentApi.get).mock.lastCall?.[2]).toEqual({ pageSizes: true })
    expect(comic.pageDimensions).toEqual([
        { width: 800, height: 1200 },
        { width: 0, height: 0 },
    ])
    await comic.dispose()
})
