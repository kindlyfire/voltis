import { afterEach, describe, expect, it, vi } from 'vitest'
import { contentApi } from '@/utils/api/content'
import { readingApi } from '@/utils/api/reading'
import type { Content, ReadingProgress, ReadingResponse, ReadingState } from '@/utils/api/types'
import { Modals } from '@/utils/modals'
import { createReadingServer } from '../fakeReadingServer'
import { resetReadingActor } from '../readingSync'
import { createComicState } from './createComicState'

vi.mock('@/utils/api/content', () => ({ contentApi: { get: vi.fn() } }))

vi.mock('@/utils/api/reading', async original => {
    const actual = await original<typeof import('@/utils/api/reading')>()
    return {
        ...actual,
        readingApi: { get: vi.fn(), post: vi.fn(), seriesReading: vi.fn() },
        invalidateReading: vi.fn(),
        markPositionSaved: vi.fn(),
        bumpContinueReading: vi.fn(),
    }
})

vi.mock('@/utils/modals', () => ({ Modals: { show: vi.fn(async () => 'start') } }))

vi.mock('./usePageLoader', async original => ({
    ...(await original<typeof import('./usePageLoader')>()),
    createPageLoader: (index: number) => {
        const loader = {
            index,
            blobUrl: null as string | null,
            loading: false,
            error: null,
            load: vi.fn(async () => {
                loader.blobUrl = `blob:${index}`
            }),
            dispose: vi.fn(),
        }
        return loader
    },
}))

afterEach(() => {
    resetReadingActor()
    vi.clearAllMocks()
    vi.useRealTimers()
})

function state(
    progress: ReadingProgress,
    status: ReadingState['status'] = 'reading'
): ReadingState {
    return {
        revision: null,
        status,
        status_updated_at: null,
        progress,
        progress_updated_at: null,
        last_read_at: null,
    }
}

function open(
    progress: ReadingProgress,
    initialPage: number | 'resume' = 'resume',
    pages: unknown[] = [0, 1, 2].map(i => [`${i}.png`, 800, 1200])
) {
    vi.mocked(contentApi.get).mockResolvedValue({
        id: 'c_1',
        parent_id: 's_1',
        file_data: { pages },
        user_data: null,
    } as unknown as Content)
    vi.mocked(readingApi.get).mockResolvedValue({
        state: state(progress),
        series: null,
        writer: null,
    })
    vi.mocked(readingApi.post).mockImplementation(
        async (_id, req) =>
            ({
                state: state(
                    'progress' in req ? req.progress : {},
                    req.op === 'clear' ? null : req.op === 'finish' ? 'completed' : 'reading'
                ),
                series: null,
                writer: req.writer_id ?? null,
                outcome: req.op === 'clear' ? 'cleared' : 'saved',
                previous: null,
                series_previous: null,
            }) as ReadingResponse
    )
    const comic = createComicState('c_1', initialPage)
    comic.setHandlers({ onReady: vi.fn(), onPlace: vi.fn() })
    return comic
}

const writes = () => vi.mocked(readingApi.post).mock.calls.map(([, req]) => req)

it('restores the saved page without writing, and writes real reading', async () => {
    vi.useFakeTimers()
    const comic = open({ current_page: 1 })
    await vi.advanceTimersByTimeAsync(2000)
    expect(comic.page).toBe(1)
    expect(writes()).toEqual([])

    comic.setPage(2)
    await vi.advanceTimersByTimeAsync(1000)
    expect(writes()).toMatchObject([
        {
            op: 'position',
            progress: { current_page: 2, progress_percent: 66.7 },
            base_revision: null,
        },
    ])

    comic.finish()
    comic.finish()
    await vi.advanceTimersByTimeAsync(0)
    expect(writes().map(w => w.op)).toEqual(['position', 'finish'])
    comic.dispose()
})

it('opens a finished comic on its last page', async () => {
    vi.useFakeTimers()
    const comic = open({ current_page: 0, at_end: true })
    await vi.advanceTimersByTimeAsync(0)
    expect(comic.page).toBe(2)
    comic.dispose()
})

it("reads the saved state after the previous reader's exit write", async () => {
    vi.useFakeTimers()
    const first = open({ current_page: 0 })
    await vi.advanceTimersByTimeAsync(0)
    first.setPage(2)
    first.dispose()
    const comic = open({ current_page: 2 })
    await vi.advanceTimersByTimeAsync(0)
    const order = (fn: (...args: never[]) => unknown) => vi.mocked(fn).mock.invocationCallOrder
    expect(order(readingApi.get)[1]).toBeGreaterThan(order(readingApi.post)[0]!)
    expect(comic.page).toBe(2)
    comic.dispose()
})

it('finishes again after a reset', async () => {
    vi.useFakeTimers()
    const comic = open({ current_page: 0 })
    await vi.advanceTimersByTimeAsync(0)
    comic.finish()
    await vi.advanceTimersByTimeAsync(0)
    await comic.sync.command({ op: 'clear' })
    comic.finish()
    await vi.advanceTimersByTimeAsync(0)
    expect(writes().map(w => w.op)).toEqual(['finish', 'clear', 'finish'])
    comic.dispose()
})

it('finishes again after accepting a clear made elsewhere', async () => {
    vi.useFakeTimers()
    const comic = open({ current_page: 0 })
    await vi.advanceTimersByTimeAsync(0)
    comic.finish()
    await vi.advanceTimersByTimeAsync(0)
    vi.mocked(readingApi.get).mockResolvedValue({
        state: { ...state({}, null), revision: 'srv:clear' },
        series: null,
        writer: null,
    })
    comic.sync.check()
    await vi.advanceTimersByTimeAsync(0)
    comic.finish()
    await vi.advanceTimersByTimeAsync(0)
    expect(writes().map(w => w.op)).toEqual(['finish', 'finish'])
    comic.dispose()
})

it('preloads ahead, and keeps the last of many pages below 100% until a finish', async () => {
    vi.useFakeTimers()
    vi.mocked(contentApi.get).mockResolvedValue({
        id: 'c_1',
        parent_id: null,
        file_data: { pages: Array.from({ length: 2000 }, (_, i) => [`${i}.png`, 800, 1200]) },
    } as unknown as Content)
    vi.mocked(readingApi.get).mockResolvedValue({
        state: state({ current_page: 0 }),
        series: null,
        writer: null,
    })
    const comic = createComicState('c_1', 'resume')
    comic.setHandlers({ onReady: vi.fn(), onPlace: vi.fn() })
    await vi.advanceTimersByTimeAsync(0)
    // Each load that settles starts the next, past the first three.
    const requested = comic.loaders.filter(l => vi.mocked(l.load).mock.calls.length)
    expect(requested.map(l => l.index)).toEqual([0, 1, 2, 3, 4, 5, 6, 7, 8, 9])
    comic.setPage(1999)
    await vi.advanceTimersByTimeAsync(1000)
    expect(writes().at(-1)).toMatchObject({ op: 'position', progress: { progress_percent: 99.9 } })
    comic.dispose()
})

describe('reopened with reading kept through a conflict', () => {
    const COMIC = {
        id: 'c_1',
        parent_id: null,
        file_data: { pages: [0, 1, 2].map(i => [`${i}.png`, 800, 1200]) },
    } as unknown as Content

    /** Reads to the end, leaves while its writes meet another device's, and reopens once the
     * comic's pages arrive; `choice` answers the conflict. */
    async function reopen(choice: string, conflictArrives: 'before' | 'while loading' = 'before') {
        vi.useFakeTimers()
        const server = createReadingServer(2)
        server.set('c_1', { status: 'reading', progress: { current_page: 0 } })
        vi.mocked(contentApi.get).mockResolvedValue(COMIC)
        const first = createComicState('c_1', 'resume')
        first.setHandlers({ onReady: vi.fn(), onPlace: vi.fn() })
        await vi.advanceTimersByTimeAsync(0)
        server.holding = true
        first.setPage(1)
        await vi.advanceTimersByTimeAsync(1000)
        first.finish()
        first.dispose()
        server.set('c_1', { progress: { current_page: 1 } })
        if (conflictArrives === 'before') server.release()
        await vi.advanceTimersByTimeAsync(0)

        const content = Promise.withResolvers<Content>()
        vi.mocked(contentApi.get).mockReturnValueOnce(content.promise)
        vi.mocked(Modals.show).mockResolvedValueOnce(choice)
        const comic = createComicState('c_1', 'resume')
        comic.setHandlers({ onReady: vi.fn(), onPlace: vi.fn() })
        // Back on the page, and the old write's 409, before the pages are known.
        window.dispatchEvent(new Event('focus'))
        server.release()
        await vi.advanceTimersByTimeAsync(0)
        expect(Modals.show).not.toHaveBeenCalled()
        expect(server.stateOf('c_1').status).toBe('reading')
        content.resolve(COMIC)
        await vi.advanceTimersByTimeAsync(0)
        expect(Modals.show).toHaveBeenCalledWith(expect.anything(), {
            kind: 'moved',
            here: 'p.3',
            saved: 'p.2',
        })
        return { server, comic }
    }

    it('asks with the real pages, and Stay sends the kept finish', async () => {
        const { server, comic } = await reopen('stay')
        expect(comic.page).toBe(2)
        expect(server.stateOf('c_1').status).toBe('completed')
        comic.dispose()
    })

    it('keeps a conflict that arrives while it loads for the load to ask about', async () => {
        const { server, comic } = await reopen('stay', 'while loading')
        expect(server.stateOf('c_1').status).toBe('completed')
        comic.dispose()
    })

    it('opens at the start when the reader chooses it', async () => {
        const { server, comic } = await reopen('start')
        expect(comic.page).toBe(0)
        expect(server.stateOf('c_1')).toMatchObject({
            status: 'reading',
            progress: { current_page: 1 },
        })
        comic.dispose()
    })
})

it('opens again after its saved state failed to load', async () => {
    vi.useFakeTimers()
    const comic = open({ current_page: 2 })
    vi.mocked(readingApi.get).mockRejectedValueOnce(new Error('down'))
    await vi.advanceTimersByTimeAsync(0)
    expect(comic.error).toBe('down')
    expect(comic.loaders).toHaveLength(0)

    comic.retry()
    expect(comic).toMatchObject({ error: null, loading: true })
    await vi.advanceTimersByTimeAsync(0)
    expect(comic).toMatchObject({ error: null, loading: false, page: 2 })
    expect(comic.loaders).toHaveLength(3)
    comic.dispose()
})

it('requests page sizes and maps unsized pages to 0, 0', async () => {
    const comic = open({ current_page: 1 }, 'resume', [['0.png', 800, 1200], ['1.png']])
    await vi.waitFor(() => expect(comic.handlers!.onReady).toHaveBeenCalled())
    expect(vi.mocked(contentApi.get).mock.lastCall?.[2]).toEqual({ pageSizes: true })
    expect(comic.pageDimensions).toEqual([
        { width: 800, height: 1200 },
        { width: 0, height: 0 },
    ])
    expect(comic.page).toBe(1)
    await comic.dispose()
})
