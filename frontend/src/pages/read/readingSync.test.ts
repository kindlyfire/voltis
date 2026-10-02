import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ToastOptions } from '@/ui/useToast'
import {
    invalidateReading,
    markPositionSaved,
    ReadingConflict,
    readingApi,
} from '@/utils/api/reading'
import type {
    Content,
    ReadingProgress,
    ReadingRequest,
    ReadingResponse,
    ReadingState,
    ReadingStateResponse,
    ReadingStatus,
} from '@/utils/api/types'
import { queryClient } from '@/utils/misc'
import { Modals } from '@/utils/modals'
import { createReadingServer } from './fakeReadingServer'
import { attachReading, resetReadingActor, type ReadingSync } from './readingSync'

vi.mock('@/utils/api/reading', async importOriginal => ({
    ...(await importOriginal<typeof import('@/utils/api/reading')>()),
    readingApi: { get: vi.fn(), post: vi.fn(), seriesReading: vi.fn() },
    invalidateReading: vi.fn(),
    markPositionSaved: vi.fn(),
    bumpContinueReading: vi.fn(),
}))
vi.mock('@/utils/modals', () => ({ Modals: { show: vi.fn() } }))
const toast = vi.fn<(options: ToastOptions) => void>()
vi.mock('@/ui/useToast', () => ({ useToast: () => ({ show: toast }) }))

const LAST = 9

let server: ReturnType<typeof createReadingServer>
const dialogs: Array<{ props: Record<string, unknown>; answer: (choice: unknown) => void }> = []
const page = (n: number): ReadingProgress => ({ current_page: n })
const settle = () => vi.advanceTimersByTimeAsync(0)
const writes = () => server.sent.flatMap(s => (s.req === 'GET' ? [] : [s.req]))
const ops = () => writes().map(w => w.op)

async function answer(choice: unknown) {
    dialogs.shift()!.answer(choice)
    await settle()
}

function toastAction(message: string) {
    const options = toast.mock.calls.findLast(([o]) => o.message.startsWith(message))?.[0]
    if (!options) throw new Error(`No toast "${message}"`)
    return () => options.action!.onClick()
}

/** A reader on item `id`, placing itself as the real ones do. */
const samePage = (a: ReadingProgress, b: ReadingProgress) =>
    (a.at_end ? LAST : a.current_page) === (b.at_end ? LAST : b.current_page)

async function open(id: string) {
    const placements: ReadingProgress[] = []
    const sync: ReadingSync = attachReading(id, {
        content: () => ({ id, title: id, parent_id: server.parents.get(id) ?? null }) as Content,
        restore(p) {
            placements.push(p)
            sync.placed(p)
        },
        describe: p => `p.${(p.current_page ?? 0) + 1}`,
        samePosition: samePage,
        samePlace: samePage,
    })
    const loaded = sync.load()
    await settle()
    return { sync, placements, opened: await loaded }
}

/** Real reading, written after the debounce. */
async function read(sync: ReadingSync, n: number) {
    sync.moved(page(n))
    await vi.advanceTimersByTimeAsync(1000)
}

function hide(hidden = true) {
    Object.defineProperty(document, 'visibilityState', {
        value: hidden ? 'hidden' : 'visible',
        configurable: true,
    })
}

beforeEach(() => {
    vi.useFakeTimers()
    resetReadingActor()
    server = createReadingServer(LAST)
    dialogs.length = 0
    toast.mockClear()
    vi.mocked(invalidateReading).mockClear()
    vi.mocked(Modals.show).mockImplementation(
        (_component, props) => new Promise(answer => dialogs.push({ props: props ?? {}, answer }))
    )
    queryClient.setQueryData(['users', 'me'], { id: 'u_a' })
    hide(false)
})

afterEach(() => {
    // Whatever happened, no two reading requests were ever out at once.
    expect(server.maxInFlight).toBeLessThanOrEqual(1)
    resetReadingActor()
    queryClient.clear()
    vi.unstubAllGlobals()
    vi.useRealTimers()
})

describe('requests', () => {
    it('sends one at a time, in order, with each base the one before acknowledged', async () => {
        const { sync } = await open('c')
        server.holding = true
        sync.moved(page(1))
        sync.finish({ current_page: LAST, at_end: true })
        void sync.command({ op: 'set_status', status: 'reading' })
        sync.check()
        await settle()
        expect(server.inFlight).toBe(1)
        for (let i = 0; i < 4; i++) {
            server.release()
            server.holding = true
            await settle()
        }
        server.release()
        await settle()
        // The check goes once the request out settles, ahead of the rest.
        expect(server.sent.map(s => (s.req === 'GET' ? 'GET' : s.req.op))).toEqual([
            'GET',
            'position',
            'GET',
            'finish',
            'set_status',
        ])
        const sent = writes()
        expect(sent[1]!.base_revision).toBe(`${sent[0]!.writer_id}:${sent[0]!.seq}`)
        expect(sent[2]!.base_revision).toBe(`${sent[1]!.writer_id}:${sent[1]!.seq}`)
    })

    it('writes nothing for placements', async () => {
        server.set('c', { status: 'reading', progress: page(4) })
        const { sync } = await open('c')
        sync.placed(page(7))
        sync.placed(page(0))
        await vi.advanceTimersByTimeAsync(5000)
        expect(writes()).toEqual([])
    })

    it('coalesces positions, but not across a finish or command', async () => {
        const { sync } = await open('c')
        sync.moved(page(1))
        sync.moved(page(2))
        sync.finish({ current_page: LAST, at_end: true })
        await settle()
        sync.moved(page(3))
        sync.moved(page(4))
        await sync.command({ op: 'set_status', status: 'on_hold' })
        expect(
            writes().map(w => [w.op, 'progress' in w ? w.progress.current_page : undefined])
        ).toEqual([
            ['position', 2],
            ['finish', LAST],
            ['position', 4],
            ['set_status', undefined],
        ])
    })

    it('ends a run of positions at a placement', async () => {
        const { sync } = await open('c')
        server.holding = true
        void sync.command({ op: 'set_status', status: 'reading' })
        await settle()
        sync.moved(page(4))
        sync.placed(page(20))
        sync.moved(page(21))
        server.release()
        await vi.advanceTimersByTimeAsync(1000)
        expect(writes().flatMap(w => ('progress' in w ? [w.progress.current_page] : []))).toEqual([
            4, 21,
        ])
    })

    it('keeps reading made while an earlier write was out', async () => {
        const { sync } = await open('c')
        server.holding = true
        await read(sync, 4)
        await read(sync, 5)
        server.release()
        await settle()
        await vi.advanceTimersByTimeAsync(1000)
        expect(server.stateOf('c').progress).toEqual(page(5))
    })

    it('sends a lost acknowledgement’s successor on top of it, without a dialog', async () => {
        const { sync } = await open('c')
        server.loseAck = true
        await read(sync, 1)
        expect(server.stateOf('c').progress).toEqual(page(1))
        await read(sync, 2)
        expect(server.stateOf('c').progress).toEqual(page(2))
        expect(dialogs).toEqual([])
        expect(sync.failures).toBe(0)
        server.loseAck = true
        await read(sync, 3)
        await sync.command({ op: 'mark_completed' })
        expect(server.stateOf('c').status).toBe('completed')
    })
})

describe('failures', () => {
    it('retries a failed save on Retry or new reading, never on focus', async () => {
        server.set('c', { status: 'reading', progress: page(0) })
        const { sync } = await open('c')
        server.offline = true
        await read(sync, 3)
        server.offline = false
        sync.check()
        await vi.advanceTimersByTimeAsync(5000)
        expect(server.stateOf('c').progress).toEqual(page(0))
        sync.retry()
        await settle()
        expect(server.stateOf('c').progress).toEqual(page(3))

        server.offline = true
        await read(sync, 4)
        await read(sync, 5)
        await read(sync, 6)
        expect(sync.failures).toBe(3)
        server.offline = false
        await read(sync, 7)
        expect(server.stateOf('c').progress).toEqual(page(7))
        expect(sync.failures).toBe(0)
    })

    it('replaces blocked reading with Mark completed, and sends it before other commands', async () => {
        const { sync } = await open('c')
        server.offline = true
        await read(sync, 3)
        server.offline = false
        await sync.command({ op: 'mark_completed' })
        sync.retry()
        await settle()
        expect(server.stateOf('c').progress).toMatchObject({ at_end: true })
        expect(ops()).toEqual(['position', 'mark_completed'])

        server.offline = true
        await read(sync, 4)
        server.offline = false
        await sync.command({ op: 'set_status', status: 'on_hold' })
        expect(server.stateOf('c')).toMatchObject({ status: 'on_hold', progress: page(4) })
    })

    it('fails a command with the older reading it would overtake, which Retry still sends', async () => {
        const { sync } = await open('c')
        server.refuse = req => req !== 'GET' && req.op === 'finish'
        sync.finish({ current_page: LAST, at_end: true })
        await settle()
        const reading = sync.command({ op: 'set_status', status: 'reading' })
        await expect(reading).rejects.toThrow()
        expect(ops()).toEqual(['finish', 'finish'])
        server.refuse = () => false
        sync.retry()
        await settle()
        await sync.command({ op: 'set_status', status: 'reading' })
        expect(ops().slice(-2)).toEqual(['finish', 'set_status'])
        expect(server.stateOf('c').status).toBe('reading')
    })

    it('offers Retry for a failed finish at once, and keeps it shown until one succeeds', async () => {
        const { sync } = await open('c')
        server.offline = true
        sync.finish({ current_page: LAST, at_end: true })
        await settle()
        expect(sync.failures).toBeGreaterThanOrEqual(3)
        sync.finish({ current_page: LAST, at_end: true })
        sync.hide()
        await settle()
        expect(ops()).toEqual(['finish'])
        sync.retry()
        await settle()
        expect(sync.failures).toBeGreaterThanOrEqual(3)
        server.offline = false
        sync.retry()
        await settle()
        expect(sync.failures).toBe(0)
        expect(server.stateOf('c').status).toBe('completed')
    })

    it('rejects a failed command', async () => {
        const { sync } = await open('c')
        server.offline = true
        await expect(sync.command({ op: 'mark_completed' })).rejects.toThrow()
    })

    it('offers Retry when moving the series to Reading fails', async () => {
        server.parents.set('c', 's')
        server.set('s', { status: 'on_hold' })
        const { sync } = await open('c')
        await read(sync, 1)
        expect(dialogs[0]!.props).toMatchObject({ status: 'on_hold', item: null })
        server.offline = true
        await answer('move')
        server.offline = false
        toastAction("Couldn't move the series")()
        await settle()
        expect(server.stateOf('s').status).toBe('reading')
        expect(sync.series?.status).toBe('reading')
    })
})

describe('hide', () => {
    it('sends unsent reading with keepalive when nothing is out', async () => {
        const { sync } = await open('c')
        sync.moved(page(3))
        sync.hide()
        await settle()
        expect(server.sent.at(-1)).toMatchObject({ keepalive: true, req: { progress: page(3) } })
    })

    it('leaves reading for later when a request is out, and loses nothing if the page lives', async () => {
        const { sync } = await open('c')
        server.holding = true
        await read(sync, 1)
        sync.moved(page(2))
        sync.hide()
        await settle()
        expect(server.sent.filter(s => s.keepalive)).toEqual([])
        server.release()
        await settle()
        expect(server.stateOf('c').progress).toEqual(page(2))
    })

    it('retries a failed keepalive on Retry', async () => {
        const { sync } = await open('c')
        server.offline = true
        sync.moved(page(3))
        sync.hide()
        await settle()
        server.offline = false
        sync.retry()
        await settle()
        expect(server.stateOf('c').progress).toEqual(page(3))
    })
})

describe('checks', () => {
    it('reads before unsent reading goes, and collapses repeats', async () => {
        const { sync, placements } = await open('c')
        sync.moved(page(4))
        sync.check()
        sync.check()
        await vi.advanceTimersByTimeAsync(1000)
        expect(server.sent.map(s => (s.req === 'GET' ? 'GET' : s.req.op))).toEqual([
            'GET',
            'GET',
            'position',
        ])
        expect(placements).toEqual([])
        expect(dialogs).toEqual([])
    })

    it('follows another device when idle, with an Undo that writes nothing', async () => {
        server.set('c', { status: 'reading', progress: page(2) })
        const { sync, placements } = await open('c')
        server.set('c', { progress: page(53) })
        sync.check()
        await settle()
        expect(placements).toEqual([page(53)])
        toastAction('Moved to p.54 from another device')()
        expect(placements).toEqual([page(53), page(2)])
        await vi.advanceTimersByTimeAsync(5000)
        expect(writes()).toEqual([])
    })

    it.each([
        ['with a position', page(2)],
        ['without one', {}],
    ])('informs of a status cleared on its own elsewhere, %s', async (_name, progress) => {
        server.set('c', { status: 'reading', progress, status_updated_at: 'then' })
        const { sync } = await open('c')
        server.set('c', { status: null, status_updated_at: 'now' })
        sync.check()
        await settle()
        expect(dialogs).toEqual([])
        expect(sync.acked?.status).toBe(null)
        expect(toast).toHaveBeenCalledWith(
            expect.objectContaining({ message: 'Status cleared on another device' })
        )
    })

    it('informs of a status changed elsewhere', async () => {
        server.set('c', { status: 'reading', progress: page(2) })
        const { sync } = await open('c')
        server.set('c', { status: 'on_hold' })
        sync.check()
        await settle()
        expect(sync.acked?.status).toBe('on_hold')
        expect(toast).toHaveBeenCalledWith(
            expect.objectContaining({ message: 'Marked On Hold on another device' })
        )
    })

    it.each([
        ['stay', 23],
        ['go', 79],
    ])('asks when both moved: %s', async (choice, saved) => {
        server.set('c', { status: 'reading', progress: page(2) })
        const { sync, placements } = await open('c')
        server.holding = true
        await read(sync, 22)
        server.set('c', { progress: page(79) })
        server.release()
        await settle()
        sync.moved(page(23))
        expect(dialogs[0]!.props).toMatchObject({ kind: 'moved', here: 'p.23', saved: 'p.80' })
        await answer(choice)
        await vi.advanceTimersByTimeAsync(1000)
        expect(server.stateOf('c').progress).toEqual(page(saved))
        expect(placements).toEqual(choice === 'go' ? [page(79)] : [])
    })

    it('drops reading whose send failed when the reader goes to the other device', async () => {
        server.set('c', { status: 'reading', progress: page(2) })
        const { sync } = await open('c')
        server.holding = true
        await read(sync, 3)
        sync.check()
        server.offline = true
        server.release()
        await settle()
        server.offline = false
        server.set('c', { progress: page(79) })
        sync.check()
        await settle()
        await answer('go')
        sync.retry()
        await vi.advanceTimersByTimeAsync(5000)
        expect(server.stateOf('c').progress).toEqual(page(79))
    })

    it('holds a conflict that came in while hidden until the page is back', async () => {
        server.set('c', { status: 'reading', progress: page(2) })
        const { sync } = await open('c')
        server.set('c', { progress: page(79) })
        hide()
        sync.moved(page(5))
        sync.hide()
        await settle()
        expect(sync.stale).toBe(true)
        expect(dialogs).toEqual([])
        hide(false)
        sync.check()
        await settle()
        await answer('stay')
        await settle()
        expect(sync.stale).toBe(false)
        expect(server.stateOf('c').progress).toEqual(page(5))
        expect(dialogs).toEqual([])
    })

    it('opens one dialog at a time across a held series, a conflict and focus', async () => {
        server.parents.set('c', 's')
        server.set('s', { status: 'dropped' })
        const { sync } = await open('c')
        await read(sync, 1)
        expect(dialogs).toHaveLength(1)
        server.set('c', { progress: page(60) })
        await read(sync, 2)
        sync.check()
        await settle()
        expect(dialogs).toHaveLength(1)
        await answer('keep')
        await settle()
        expect(dialogs).toHaveLength(1)
        expect(dialogs[0]!.props).toMatchObject({ kind: 'moved' })
        await answer('stay')
        await settle()
        expect(server.stateOf('c').progress).toEqual(page(2))
        expect(server.stateOf('s').status).toBe('dropped')
    })

    it('checks only once the open dialog is answered', async () => {
        server.parents.set('c', 's')
        server.set('s', { status: 'on_hold' })
        const { sync, placements } = await open('c')
        await read(sync, 1)
        server.clear('c')
        sync.check()
        await settle()
        expect(dialogs).toHaveLength(1)
        await answer('keep')
        expect(dialogs[0]!.props).toMatchObject({ kind: 'reset' })
        await answer('start')
        expect(placements).toEqual([{}])
    })

    it('keeps a clear acknowledged after a check that read the state before it', async () => {
        server.set('c', { status: 'reading', progress: page(3) })
        const { sync, placements } = await open('c')
        sync.check()
        sync.resetAndReadAgain()
        await settle()
        await answer(true)
        expect(server.stateOf('c').status).toBe(null)
        expect(placements).toEqual([{}])
    })

    it('asks again after a clear that conflicted, against the new state', async () => {
        server.set('c', { status: 'reading', progress: page(3) })
        const { sync, placements } = await open('c')
        sync.resetAndReadAgain()
        await settle()
        server.set('c', { status: 'on_hold' })
        await answer(true)
        expect(toast).toHaveBeenCalledWith(expect.objectContaining({ tone: 'danger' }))
        expect(sync.acked?.status).toBe('on_hold')
        sync.resetAndReadAgain()
        await settle()
        await answer(true)
        expect(server.stateOf('c').status).toBe(null)
        expect(placements).toEqual([{}])
        expect(ops()).toEqual(['clear', 'clear'])
    })

    it('compares an item changed elsewhere when a series command returns', async () => {
        server.parents.set('c', 's')
        server.set('s', { status: 'on_hold' })
        server.set('c', { status: 'reading', progress: page(2) })
        const { sync, placements } = await open('c')
        server.set('c', { progress: page(79) })
        await sync.seriesCommand('reading')
        expect(placements).toEqual([page(79)])
        sync.moved(page(3))
        server.set('c', { progress: page(60) })
        const command = sync.seriesCommand('reading')
        await settle()
        expect(dialogs[0]!.props).toMatchObject({ kind: 'moved', saved: 'p.61' })
        await answer('go')
        await command
        await vi.advanceTimersByTimeAsync(1000)
        expect(server.stateOf('c').progress).toEqual(page(60))
    })

    it('keeps reading a conflict met after the reader left, and asks on reopening', async () => {
        server.set('c', { status: 'reading', progress: page(2) })
        const first = await open('c')
        server.holding = true
        void first.sync.command({ op: 'set_status', status: 'reading' }).catch(() => null)
        await settle()
        first.sync.finish({ current_page: LAST, at_end: true })
        first.sync.detach()
        server.set('c', { progress: page(40) })
        server.release()
        await settle()
        expect(server.stateOf('c').status).toBe('reading')
        await open('d')
        const again = open('c')
        await settle()
        expect(dialogs[0]!.props).toMatchObject({ kind: 'moved', here: 'p.10', saved: 'p.41' })
        await answer('stay')
        await again
        expect(server.stateOf('c').status).toBe('completed')
    })
})

describe('finish', () => {
    it('is taken once, and reports at the finished position are not reading on', async () => {
        const { sync } = await open('c')
        sync.finish({ current_page: LAST, at_end: true })
        sync.moved(page(LAST))
        sync.finish({ current_page: LAST, at_end: true })
        await vi.advanceTimersByTimeAsync(2000)
        expect(ops()).toEqual(['finish'])
        expect(server.stateOf('c').progress).toMatchObject({ at_end: true })
    })

    it.each(['acknowledged', 'still out'])(
        'reads a turn back right after finishing, the finish %s',
        async state => {
            const { sync } = await open('c')
            server.holding = state === 'still out'
            sync.finish({ current_page: LAST, at_end: true })
            await settle()
            sync.moved(page(LAST - 1))
            sync.hide()
            server.release()
            await settle()
            expect(ops()).toEqual(['finish', 'position'])
            expect(server.stateOf('c')).toMatchObject({
                status: 'completed',
                progress: page(LAST - 1),
            })
        }
    )

    it.each(['acknowledged', 'still queued'])(
        'leaves a completed item read again at its bookmark, the positions %s',
        async state => {
            server.set('c', { status: 'completed', progress: { current_page: LAST, at_end: true } })
            const { sync, opened } = await open('c')
            expect(opened).toMatchObject({ at_end: true })
            sync.moved(page(3))
            if (state === 'acknowledged') await vi.advanceTimersByTimeAsync(1500)
            sync.moved(page(LAST))
            if (state === 'acknowledged') await vi.advanceTimersByTimeAsync(1500)
            sync.finish({ current_page: LAST, at_end: true })
            await settle()
            // Acknowledged, the finish isn't sent; queued, the server makes it nothing.
            expect(ops().at(-1)).toBe(state === 'acknowledged' ? 'position' : 'finish')
            expect(server.stateOf('c')).toMatchObject({ status: 'completed', progress: page(LAST) })
            expect(server.stateOf('c').progress).not.toHaveProperty('at_end')

            sync.detach()
            expect((await open('c')).opened).toEqual(page(LAST))
        }
    )

    it('is taken again once set back to Reading', async () => {
        const { sync } = await open('c')
        sync.finish({ current_page: LAST, at_end: true })
        await settle()
        await sync.command({ op: 'set_status', status: 'reading' })
        sync.finish({ current_page: LAST, at_end: true })
        await settle()
        expect(ops().filter(op => op === 'finish')).toHaveLength(2)
    })

    // A one-page comic: the finish, the place it is dropped for and the place after are one page.
    it('is taken again once a clear drops a failed finish', async () => {
        const { sync } = await open('c')
        server.offline = true
        sync.finish({ current_page: LAST, at_end: true })
        await settle()
        server.offline = false
        await sync.command({ op: 'clear' })
        sync.placed(page(LAST))
        sync.finish({ current_page: LAST, at_end: true })
        await settle()
        expect(ops()).toEqual(['finish', 'clear', 'finish'])
        expect(server.stateOf('c')).toMatchObject({ status: 'completed' })
    })

    it('survives a conflict the reader stays through', async () => {
        server.set('c', { status: 'reading', progress: page(2) })
        const { sync } = await open('c')
        server.set('c', { progress: page(5) })
        sync.finish({ current_page: LAST, at_end: true })
        await settle()
        await answer('stay')
        await settle()
        expect(server.stateOf('c').status).toBe('completed')
    })

    it.each(['here', 'elsewhere'])('is taken again after a clear made %s', async where => {
        const { sync, placements } = await open('c')
        sync.finish({ current_page: LAST, at_end: true })
        await settle()
        if (where === 'here') {
            sync.resetAndReadAgain()
            await settle()
            await answer(true)
        } else {
            server.clear('c')
            sync.check()
            await settle()
            expect(dialogs[0]!.props).toMatchObject({ kind: 'reset' })
            await answer('start')
        }
        expect(placements).toEqual([{}])
        sync.finish({ current_page: LAST, at_end: true })
        await settle()
        expect(server.stateOf('c').status).toBe('completed')
    })

    it('reads a deliberate turn after a placement back from the end', async () => {
        const { sync } = await open('c')
        sync.finish({ current_page: LAST, at_end: true })
        await settle()
        sync.placed(page(0))
        await read(sync, 1)
        expect(server.stateOf('c').progress).toEqual(page(1))
    })

    it('gets the series as completion left it', async () => {
        server.parents.set('c', 's')
        server.parents.set('d', 's')
        server.set('d', { status: 'completed' })
        server.set('s', { status: 'reading' })
        const { sync } = await open('c')
        sync.finish({ current_page: LAST, at_end: true })
        await settle()
        expect(sync.series).toMatchObject({ caught_up: true, completed_children_count: 2 })
    })
})

describe('undo', () => {
    it('restores, stops tracking, and works after leaving and reopening', async () => {
        server.parents.set('c', 's')
        server.set('c', { status: 'plan_to_read' })
        const first = await open('c')
        await read(first.sync, 1)
        expect(server.stateOf('s').status).toBe('reading')
        first.sync.detach()
        await open('d')
        const again = await open('c')
        toastAction('Moved to Reading')()
        await settle()
        expect(server.stateOf('c').status).toBe('plan_to_read')
        expect(server.stateOf('s').status).toBe(null)
        expect(again.sync.tracking).toBe(false)
        await read(again.sync, 2)
        expect(ops()).toEqual(['position', 'restore'])
        again.sync.trackProgress()
        await read(again.sync, 3)
        expect(server.stateOf('c').status).toBe('reading')
    })

    it('puts back a completed series that starting a volume reopened, alone', async () => {
        server.parents.set('c', 's')
        server.set('s', { status: 'completed', status_updated_at: 'then' })
        const { sync } = await open('c')
        await read(sync, 1)
        expect(server.stateOf('s').status).toBe('reading')
        toastAction('Series moved to Reading')()
        await settle()
        expect(server.stateOf('s')).toMatchObject({
            status: 'completed',
            status_updated_at: 'then',
        })
        expect(server.stateOf('c').status).toBe('reading')
        expect(sync.tracking).toBe(true)
    })

    it('offers Retry when it fails', async () => {
        server.set('c', { status: 'on_hold' })
        const { sync } = await open('c')
        await read(sync, 1)
        server.offline = true
        toastAction('Moved to Reading')()
        await settle()
        server.offline = false
        toastAction("Couldn't undo")()
        await settle()
        expect(server.stateOf('c').status).toBe('on_hold')
    })

    it('is offered by the held-series prompt instead of a toast', async () => {
        server.parents.set('c', 's')
        server.set('s', { status: 'on_hold' })
        server.set('c', { status: 'dropped' })
        const { sync } = await open('c')
        sync.finish({ current_page: LAST, at_end: true })
        await settle()
        expect(dialogs[0]!.props).toMatchObject({ item: 'c marked completed.' })
        await answer('undo')
        expect(server.stateOf('c').status).toBe('dropped')
        expect(toast).not.toHaveBeenCalled()
    })
})

describe('series commands', () => {
    it('adopt the series as the server has it after each read', async () => {
        server.parents.set('c', 's')
        server.set('s', { status: 'on_hold' })
        const { sync } = await open('c')
        await sync.seriesCommand('reading')
        expect(sync.series?.status).toBe('reading')
        server.set('s', { status: 'dropped' })
        sync.check()
        await settle()
        expect(sync.series?.status).toBe('dropped')
    })
})

describe('series completion', () => {
    it('asks, then completes in turn, adopting its own write to the item', async () => {
        server.parents.set('c', 's')
        server.parents.set('d', 's')
        server.set('s', { status: 'reading' })
        const { sync } = await open('c')
        server.holding = true
        await read(sync, 3)
        sync.completeSeries()
        await settle()
        expect(dialogs[0]!.props).toMatchObject({
            seriesId: 's',
            known: { type: 'comic_series', unread: 2 },
        })
        await answer({ includeUnread: true })
        server.release()
        await settle()
        expect(server.stateOf('d').status).toBe('completed')
        expect(sync.acked?.status).toBe('completed')
        expect(sync.series?.status).toBe('completed')
        expect(dialogs).toEqual([])
        expect(toast).toHaveBeenCalledWith({ message: 'Marked the series completed' })
    })

    async function failTwice(sync: ReadingSync) {
        server.offline = true
        await read(sync, 3)
        await read(sync, 4)
        server.offline = false
    }

    it.each([true, false])(
        'settles failed reading first, never replayed over it (unread volumes too: %s)',
        async includeUnread => {
            server.parents.set('c', 's')
            const { sync } = await open('c')
            await failTwice(sync)
            sync.completeSeries()
            await settle()
            await answer({ includeUnread })
            await settle()
            sync.retry()
            await vi.advanceTimersByTimeAsync(2000)
            expect(server.stateOf('s').status).toBe('completed')
            // Completed with the series, or saved before it.
            expect(server.stateOf('c')).toMatchObject(
                includeUnread
                    ? { status: 'completed', progress: { at_end: true } }
                    : { status: 'reading', progress: page(4) }
            )
        }
    )

    it.each([
        ['it covers', 'reading', { status: 'completed', progress: { at_end: true } }],
        ["it doesn't cover", 'completed', { status: 'completed', progress: page(4) }],
    ])("drops a sibling volume's failed reading only where %s", async (_how, status, after) => {
        server.parents.set('c', 's')
        server.parents.set('d', 's')
        server.set('d', { status: status as ReadingStatus, progress: page(1) })
        const d = await open('d')
        await failTwice(d.sync)
        d.sync.detach()
        const { sync } = await open('c')
        sync.completeSeries()
        await settle()
        await answer({ includeUnread: true })
        await settle()
        const again = await open('d')
        again.sync.retry()
        await vi.advanceTimersByTimeAsync(2000)
        expect(server.stateOf('d')).toMatchObject(after)
    })
})

describe('caches', () => {
    it('refetch after status changes at once, and after positions once the reader leaves', async () => {
        server.parents.set('c', 's')
        const { sync } = await open('c')
        await read(sync, 1)
        expect(invalidateReading).toHaveBeenCalledTimes(1)
        await read(sync, 2)
        expect(invalidateReading).toHaveBeenCalledTimes(1)
        expect(markPositionSaved).toHaveBeenLastCalledWith('c', 's')
        sync.detach()
        expect(invalidateReading).toHaveBeenLastCalledWith('c', 's')
        expect(invalidateReading).toHaveBeenCalledTimes(2)
    })

    it('let the next reader read after the last one’s exit write', async () => {
        const { sync } = await open('c')
        sync.moved(page(6))
        sync.detach()
        const next = await open('c')
        expect(next.sync.acked?.progress).toEqual(page(6))
    })
})

describe('account switch', () => {
    it('freezes, ignores late responses and reloads the page', async () => {
        const reload = vi.fn()
        vi.stubGlobal('location', { reload })
        server.set('c', { status: 'on_hold' })
        const { sync } = await open('c')
        server.holding = true
        await read(sync, 1)
        sync.moved(page(2))
        queryClient.setQueryData(['users', 'me'], { id: 'u_b' })
        expect(reload).toHaveBeenCalled()
        server.release()
        await vi.advanceTimersByTimeAsync(5000)
        expect(ops()).toEqual(['position'])
        expect(toast).not.toHaveBeenCalled()
        expect(dialogs).toEqual([])
    })

    it('stops at sign-out, rejecting what waits, and reloads at the next sign-in', async () => {
        const reload = vi.fn()
        vi.stubGlobal('location', { reload })
        const { sync } = await open('c')
        server.holding = true
        const out = sync.command({ op: 'set_status', status: 'reading' })
        await settle()
        const waiting = sync.command({ op: 'mark_completed' })
        queryClient.setQueryData(['users', 'me'], null)
        // The request out is still held: its promise doesn't wait for it.
        await expect(out).rejects.toThrow()
        await expect(waiting).rejects.toThrow()
        await expect(attachReading('d', {} as never).load()).rejects.toThrow()
        expect(reload).not.toHaveBeenCalled()
        queryClient.setQueryData(['users', 'me'], { id: 'u_a' })
        expect(reload).toHaveBeenCalled()
        server.release()
        await settle()
        expect(toast).not.toHaveBeenCalled()
    })

    it('ignores a check out at the switch', async () => {
        vi.stubGlobal('location', { reload: vi.fn() })
        server.set('c', { status: 'reading', progress: page(1) })
        const { sync, placements } = await open('c')
        server.holding = true
        sync.check()
        await settle()
        server.set('c', { progress: page(8) })
        queryClient.setQueryData(['users', 'me'], null)
        server.release()
        await settle()
        expect(placements).toEqual([])
        expect(sync.acked?.progress).toEqual(page(1))
    })
})

describe('reading out when something replaces it', () => {
    // A save of p.4 that's out when `replace` runs, and fails after: it goes nowhere again.
    async function failsAfter(
        setup: () => Promise<ReadingSync>,
        replace: (sync: ReadingSync) => Promise<void>
    ) {
        const sync = await setup()
        server.holding = true
        await read(sync, 4)
        await settle()
        const replaced = replace(sync)
        await settle()
        server.refuse = req => req !== 'GET' && req.op === 'position'
        server.release()
        await replaced
        await settle()
        server.refuse = () => false
        expect(sync.failures).toBe(0)
        sync.retry()
        await vi.advanceTimersByTimeAsync(2000)
        return sync
    }
    const positionsAfter = (op: ReturnType<typeof ops>[number]) =>
        ops()
            .slice(ops().lastIndexOf(op) + 1)
            .filter(o => o === 'position')

    it('is dropped for a clear', async () => {
        await failsAfter(
            async () => (await open('c')).sync,
            sync => sync.command({ op: 'clear' })
        )
        expect(positionsAfter('clear')).toEqual([])
        expect(server.stateOf('c')).toMatchObject({ status: null, progress: {} })
    })

    it('is dropped for Mark completed, and stays dropped through the next command', async () => {
        const sync = await failsAfter(
            async () => (await open('c')).sync,
            sync => sync.command({ op: 'mark_completed' })
        )
        await sync.command({ op: 'set_status', status: 'reading' })
        await settle()
        expect(positionsAfter('mark_completed')).toEqual([])
        expect(server.stateOf('c')).toMatchObject({ status: 'reading', progress: { at_end: true } })
    })

    it('is dropped for an Undo', async () => {
        server.set('c', { status: 'on_hold' })
        await failsAfter(
            async () => {
                const { sync } = await open('c')
                await read(sync, 1)
                return sync
            },
            async () => toastAction('Moved to Reading')()
        )
        expect(positionsAfter('restore')).toEqual([])
        expect(server.stateOf('c')).toMatchObject({ status: 'on_hold' })
    })

    it('is dropped for a series completion that covers it', async () => {
        server.parents.set('c', 's')
        await failsAfter(
            async () => (await open('c')).sync,
            async sync => {
                sync.completeSeries()
                await settle()
                await answer({ includeUnread: true })
            }
        )
        expect(server.stateOf('c')).toMatchObject({
            status: 'completed',
            progress: { at_end: true },
        })
        expect(server.sent.filter(s => s.req !== 'GET' && s.req.op === 'position')).toHaveLength(1)
    })
})
