import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, onTestFinished, vi } from 'vitest'
import { effectScope, nextTick } from 'vue'
import { invalidateCatalog, type ScanRow, scanRow, useScanStore, useScanSync } from '@/stores/scans'
import { tasksApi } from '@/utils/api/tasks'
import type { ScanProgress, TaskLogs, TaskSnapshot } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'
import { queryClient } from '@/utils/misc'
import { ws } from '@/utils/ws'

vi.mock('@/utils/api/tasks', () => ({
    tasksApi: { snapshot: vi.fn(), logs: vi.fn() },
}))

vi.mock('@/utils/api/users', async () => {
    const { ref } = await import('vue')
    const data = ref<{ permissions: string[] } | null>(null)
    return { usersApi: { useMe: () => ({ data }), meData: data } }
})

vi.mock('@/utils/ws', () => {
    const handlers = new Map<string, Set<(msg: any) => void>>()
    return {
        ws: {
            connect: () => {},
            send: () => {},
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

const snapshotMock = vi.mocked(tasksApi.snapshot)
const logsMock = vi.mocked(tasksApi.logs)
const meData = (usersApi as unknown as { meData: { value: { permissions: string[] } | null } })
    .meData
const emit = (ws as unknown as { emit: (type: string, msg: any) => void }).emit

const progress: ScanProgress = {
    phase: 'parsing',
    found: 10,
    total: 4,
    processed: 1,
    unchanged: 6,
    failed: 0,
    saved: { added: 0, updated: 0, removed: 0 },
    commit_seq: 0,
}

function task(over: Partial<TaskSnapshot> = {}): TaskSnapshot {
    return {
        id: 't_1',
        name: 'scan_library',
        status: 1,
        input: {
            library_id: 'l_1',
            library_type: 'comics',
            sources: ['/comics'],
            force: false,
        },
        output: {},
        progress,
        log_len: 0,
        created_at: '2026-09-18T08:00:00Z',
        updated_at: '2026-09-18T08:00:01Z',
        ...over,
    }
}

beforeEach(() => {
    setActivePinia(createPinia())
    snapshotMock.mockReset()
    logsMock.mockReset()
    snapshotMock.mockResolvedValue([])
    meData.value = null
})

afterEach(() => {
    vi.useRealTimers()
})

function startSync(): void {
    const scope = effectScope()
    scope.run(() => useScanSync())
    onTestFinished(() => scope.stop())
}

describe('shared store', () => {
    it('gives every holder one instance, so a dismissal is visible to all of them', () => {
        const modal = useScanStore()
        const indicator = useScanStore()

        modal.accept(task())
        expect(indicator.tasks['t_1']).toBe(modal.tasks['t_1'])

        indicator.dismiss(['t_1'])
        expect(modal.dismissed.has('t_1')).toBe(true)
        expect(modal.tasks['t_1']).toBeDefined()
    })
})

describe('scanRow', () => {
    it.each([
        {
            name: 'never divides by a zero total',
            over: { progress: { ...progress, total: 0, processed: 0 } },
            want: { value: 100, detail: '0 / 0' },
        },
        {
            name: 'shows found counts while walking',
            over: { progress: { ...progress, phase: 'walking', found: 21 } },
            want: { indeterminate: true, detail: 'Looking for files, 21 found' },
        },
        {
            name: 'shows saved counts while saving',
            over: {
                progress: {
                    ...progress,
                    phase: 'saving',
                    saved: { added: 3, updated: 2, removed: 1 },
                },
            },
            want: { detail: '3 added, 2 updated, 1 removed' },
        },
        {
            name: 'uses the terminal output counts',
            over: {
                status: 2,
                progress: null,
                output: { added: 5, updated: 1, removed: 2, failed: 0, unchanged: 9, duration: 1 },
            },
            want: { detail: '5 added, 1 updated, 2 removed', color: 'success' },
        },
        {
            name: 'reports queued tasks',
            over: { status: 0, progress: null },
            want: { detail: 'Queued' },
        },
    ] as { name: string; over: Partial<TaskSnapshot>; want: Partial<ScanRow> }[])(
        '$name',
        ({ over, want }) => {
            expect(scanRow(task(over))).toMatchObject(want)
        }
    )
})

describe('accept', () => {
    it('recovers a missed terminal event through reconcile', async () => {
        const store = useScanStore()
        store.accept(task())

        snapshotMock.mockResolvedValue([
            task({
                status: 2,
                progress: null,
                output: {
                    added: 2,
                    updated: 0,
                    removed: 0,
                    failed: 0,
                    unchanged: 6,
                    duration: 3,
                },
                updated_at: '2026-09-18T08:00:09Z',
            }),
        ])
        await store.reconcile(['t_1'])

        expect(snapshotMock).toHaveBeenCalledWith(['t_1'])
        expect(store.tasks['t_1']!.status).toBe(2)
        expect(scanRow(store.tasks['t_1']!).detail).toBe('2 added, 0 updated, 0 removed')
    })

    it('keeps the socket snapshot when a slower POST reconcile returns an older row', async () => {
        const store = useScanStore()
        startSync()
        meData.value = { permissions: ['ADMIN'] }
        await nextTick()

        const { promise, resolve } = Promise.withResolvers<TaskSnapshot[]>()
        snapshotMock.mockReturnValue(promise)
        const pending = store.reconcile(['t_1'])

        emit('task_update', {
            task: task({
                status: 2,
                output: { added: 1, updated: 0, removed: 0, failed: 0, unchanged: 0, duration: 1 },
                updated_at: '2026-09-18T08:00:20Z',
            }),
        })
        expect(store.tasks['t_1']!.status).toBe(2)

        resolve([task({ status: 1, updated_at: '2026-09-18T08:00:02Z' })])
        await pending

        expect(store.tasks['t_1']!.status).toBe(2)
        expect(store.tasks['t_1']!.updated_at).toBe('2026-09-18T08:00:20Z')
    })
})

describe('log tails', () => {
    it('appends by UTF-8 byte offset and never by string length', async () => {
        const store = useScanStore()
        logsMock.mockResolvedValueOnce({ offset: 0, text: 'héllo', len: 6 })
        await store.fetchLogs('t_1')

        expect(logsMock).toHaveBeenCalledWith('t_1', 0)
        expect(store.logs['t_1']).toEqual({ text: 'héllo', len: 6 })

        logsMock.mockResolvedValueOnce({ offset: 6, text: ' wörld', len: 13 })
        await store.fetchLogs('t_1')

        expect(logsMock).toHaveBeenLastCalledWith('t_1', 6)
        expect(store.logs['t_1']).toEqual({ text: 'héllo wörld', len: 13 })
    })

    it('ignores a response that does not start at the held offset', async () => {
        const store = useScanStore()
        logsMock.mockResolvedValueOnce({ offset: 0, text: 'abc', len: 3 })
        await store.fetchLogs('t_1')

        logsMock.mockResolvedValueOnce({ offset: 1, text: 'bc', len: 3 })
        await store.fetchLogs('t_1')

        expect(store.logs['t_1']).toEqual({ text: 'abc', len: 3 })
    })
})

describe('catalog invalidation', () => {
    it('throttles to 500ms with leading and trailing edges', async () => {
        vi.useFakeTimers()
        const spy = vi.spyOn(queryClient, 'invalidateQueries').mockImplementation(async () => {})

        invalidateCatalog()
        expect(spy).toHaveBeenCalledTimes(2)
        expect(spy).toHaveBeenCalledWith({ queryKey: ['libraries'] })
        expect(spy).toHaveBeenCalledWith({ queryKey: ['content'] })

        invalidateCatalog()
        invalidateCatalog()
        expect(spy).toHaveBeenCalledTimes(2)

        await vi.advanceTimersByTimeAsync(500)
        expect(spy).toHaveBeenCalledTimes(4)

        await vi.advanceTimersByTimeAsync(2000)
        expect(spy).toHaveBeenCalledTimes(4)
        spy.mockRestore()
    })
})

describe('useScanSync', () => {
    it('reconciles for admins, follows socket updates and resets on logout', async () => {
        const store = useScanStore()
        startSync()

        meData.value = { permissions: ['ADMIN'] }
        await nextTick()
        expect(snapshotMock).toHaveBeenCalledWith(undefined)

        emit('task_update', { task: task() })
        expect(store.tasks['t_1']).toBeDefined()

        snapshotMock.mockClear()
        emit('$open', {})
        expect(snapshotMock).toHaveBeenCalledWith(['t_1'])

        meData.value = null
        await nextTick()
        expect(store.tasks).toEqual({})
        expect(store.logs).toEqual({})

        snapshotMock.mockClear()
        emit('task_update', { task: task() })
        expect(store.tasks['t_1']).toBeUndefined()
    })

    it('swallows reconcile failures instead of leaving the promise unhandled', async () => {
        startSync()

        snapshotMock.mockRejectedValue(new Error('network'))
        meData.value = { permissions: ['ADMIN'] }
        await nextTick()
        emit('$open', {})
        await new Promise(resolve => setTimeout(resolve, 0))

        expect(snapshotMock).toHaveBeenCalledTimes(2)
    })

    it('leaves non-admins with no task data', async () => {
        const store = useScanStore()
        startSync()

        meData.value = { permissions: [] }
        await nextTick()

        expect(snapshotMock).not.toHaveBeenCalled()
        emit('task_update', { task: task() })
        expect(store.tasks['t_1']).toBeUndefined()
    })
})

describe('reset cancellation', () => {
    it('drops snapshot and log responses that were in flight when reset ran', async () => {
        const store = useScanStore()

        const snapshot = Promise.withResolvers<TaskSnapshot[]>()
        snapshotMock.mockReturnValue(snapshot.promise)
        const reconciling = store.reconcile(['t_1'])

        const logs = Promise.withResolvers<TaskLogs>()
        logsMock.mockReturnValue(logs.promise)
        const fetching = store.fetchLogs('t_1')

        store.reset()

        snapshot.resolve([task()])
        logs.resolve({ offset: 0, text: '/comics/private.cbz failed\n', len: 27 })
        await reconciling
        await fetching

        expect(store.tasks).toEqual({})
        expect(store.logs).toEqual({})
    })

    it('never lets an older log request clear a newer request guard', async () => {
        const store = useScanStore()
        logsMock.mockReturnValue(new Promise(() => {}))

        const held = Promise.withResolvers<TaskLogs>()
        logsMock.mockReturnValueOnce(held.promise)
        const first = store.fetchLogs('t_1')

        store.reset()

        const second = store.fetchLogs('t_1')
        expect(logsMock).toHaveBeenCalledTimes(2)

        held.resolve({ offset: 0, text: 'abc', len: 3 })
        await first

        void store.fetchLogs('t_1')
        expect(logsMock).toHaveBeenCalledTimes(2)
        void second
    })
})

describe('timestamp ordering', () => {
    const terminal = {
        status: 2,
        output: { added: 1, updated: 0, removed: 0, failed: 0, unchanged: 0, duration: 1 },
    } as Partial<TaskSnapshot>

    it.each([
        {
            name: 'rejects an older whole-second REST row and keeps live progress',
            updates: [
                { updated_at: '2026-09-18T08:00:10Z' },
                { status: 0, progress: null, updated_at: '2026-09-18T08:00:05Z' },
            ],
            want: { status: 1, progress },
        },
        {
            name: 'keeps terminal output when a later row carries no progress',
            updates: [
                {
                    status: 2,
                    output: {
                        added: 4,
                        updated: 0,
                        removed: 0,
                        failed: 0,
                        unchanged: 0,
                        duration: 2,
                    },
                    updated_at: '2026-09-18T08:00:10Z',
                },
                { status: 2, progress: null, updated_at: '2026-09-18T08:00:11Z' },
            ],
            want: {
                output: { added: 4, updated: 0, removed: 0, failed: 0, unchanged: 0, duration: 2 },
                progress,
            },
        },
        {
            name: 'orders sub-second timestamps by instant and not by string',
            updates: [
                { updated_at: '2026-09-18T08:00:01.123Z' },
                { ...terminal, progress: null, updated_at: '2026-09-18T08:00:01.1234Z' },
                { status: 1, updated_at: '2026-09-18T08:00:01.12Z' },
            ],
            want: { status: 2, updated_at: '2026-09-18T08:00:01.1234Z' },
        },
        {
            name: 'rejects an older timestamp inside the same millisecond',
            updates: [
                { updated_at: '2026-09-18T08:00:01.5009Z' },
                { status: 4, updated_at: '2026-09-18T08:00:01.5001Z' },
            ],
            want: { status: 1, updated_at: '2026-09-18T08:00:01.5009Z' },
        },
        {
            name: 'accepts an equal timestamp',
            updates: [
                { updated_at: '2026-09-18T08:00:01.5Z' },
                { status: 4, updated_at: '2026-09-18T08:00:01.5Z' },
            ],
            want: { status: 4 },
        },
    ] as { name: string; updates: Partial<TaskSnapshot>[]; want: Partial<TaskSnapshot> }[])(
        '$name',
        ({ updates, want }) => {
            const store = useScanStore()
            for (const over of updates) store.accept(task(over))
            expect(store.tasks['t_1']).toMatchObject(want)
        }
    )
})
