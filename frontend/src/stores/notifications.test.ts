import { flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, onTestFinished, vi } from 'vitest'
import { effectScope, nextTick } from 'vue'
import { useNotificationStore, useNotificationSync } from '@/stores/notifications'
import { notificationsApi } from '@/utils/api/notifications'
import type { Notification, NotificationFeed } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'
import { ws } from '@/utils/ws'

vi.mock('@/utils/api/notifications', () => ({
    notificationsApi: {
        feed: vi.fn(),
        seen: vi.fn(),
        dismiss: vi.fn(),
        clear: vi.fn(),
    },
}))

vi.mock('@/utils/api/users', async () => {
    const { ref } = await import('vue')
    const data = ref<{ id: string } | null>(null)
    return { usersApi: { useMe: () => ({ data }), meData: data } }
})

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

const feedMock = vi.mocked(notificationsApi.feed)
const seenMock = vi.mocked(notificationsApi.seen)
const dismissMock = vi.mocked(notificationsApi.dismiss)
const clearMock = vi.mocked(notificationsApi.clear)
const meData = (usersApi as unknown as { meData: { value: { id: string } | null } }).meData
const emit = (ws as unknown as { emit: (type: string, msg: any) => void }).emit

function feed(over: Partial<NotificationFeed> = {}): NotificationFeed {
    return {
        active: [],
        needs_input: [],
        data: [],
        unread: 0,
        next: null,
        ...over,
    }
}

function series(id: string, over: Partial<Notification> = {}): Notification {
    return {
        id,
        kind: 'new_content',
        created_at: '2026-10-14T10:00:00Z',
        updated_at: '2026-10-14T10:00:00Z',
        alerted_at: null,
        unread: false,
        task_id: null,
        content: { id: 'c_1', title: 'Moon', type: 'comic_series' },
        data: { count: 1, unit: 'chapter' },
        ...over,
    } as Notification
}

function failedScan(alertedAt: string): Notification {
    return series('n_scan', {
        kind: 'scan',
        task_id: 't_1',
        alerted_at: alertedAt,
        unread: true,
        content: null,
        data: { library_id: 'l_1', library_name: 'Comics', outcome: 'failed' },
    } as Partial<Notification>)
}

function many(n: number, prefix = 'h'): Notification[] {
    return Array.from({ length: n }, (_, i) => series(`${prefix}${i}`))
}

const ids = (list: Notification[]) => list.map(i => i.id)

beforeEach(() => {
    setActivePinia(createPinia())
    feedMock.mockReset()
    seenMock.mockReset()
    feedMock.mockResolvedValue(feed())
    meData.value = null
})

async function startSync() {
    const scope = effectScope()
    scope.run(() => useNotificationSync())
    onTestFinished(() => scope.stop())
    meData.value = { id: 'u_1' }
    await nextTick()
    await flushPromises()
}

describe('a watched scan', () => {
    it.each([
        ['notifications_changed', 'task_update', false],
        ['task_update', 'notifications_changed', false],
        ['task_update', 'notifications_changed', true],
    ])(
        'stays out of the badge and is marked seen when %s comes first (unloaded: %s)',
        async (first, second, unloaded) => {
            const store = useNotificationStore()
            await startSync()
            const unwatch = store.watch(['t_1'])
            const scan = failedScan('2026-10-14T10:00:00.123456Z')
            // Unloaded: the pages hold other items, and the scan comes from a fetch by task.
            feedMock.mockImplementation(async params =>
                params?.taskId
                    ? feed({ data: [scan] })
                    : feed({ data: unloaded ? many(3) : [scan], unread: 1 })
            )
            const seen = Promise.withResolvers<{ unread: number }>()
            seenMock.mockReturnValue(seen.promise)

            const messages: Record<string, any> = {
                notifications_changed: { unread: 7 },
                task_update: { task: { id: 't_1', status: 3 } },
            }
            emit(first, messages[first])
            emit(second, messages[second])
            await flushPromises()

            expect(seenMock).toHaveBeenCalledTimes(1)
            expect(seenMock).toHaveBeenCalledWith([
                { id: 'n_scan', alerted_at: '2026-10-14T10:00:00.123456Z' },
            ])
            expect(store.badge).toBe(0)

            // Closing the modal first doesn't bring the alert back.
            unwatch()
            expect(store.badge).toBe(0)
            seen.resolve({ unread: 0 })
            await flushPromises()
            expect(store.badge).toBe(0)
            expect(store.items[0]!.unread).toBe(false)
        }
    )
})

describe('a scan that ended before it was watched', () => {
    it('is marked seen when the watch starts', async () => {
        const store = useNotificationStore()
        feedMock.mockResolvedValue(
            feed({ data: [failedScan('2026-10-14T10:00:00.123456Z')], unread: 1 })
        )
        seenMock.mockResolvedValue({ unread: 0 })
        await startSync()
        expect(store.badge).toBe(1)

        store.watch(['t_1'])
        expect(store.badge).toBe(0)
        await flushPromises()
        expect(seenMock).toHaveBeenCalledTimes(1)
        expect(store.items[0]!.unread).toBe(false)
    })
})

describe('unread counts', () => {
    it('takes them from feed responses and not from pushes, and keeps a later alert unread', async () => {
        const store = useNotificationStore()
        await startSync()
        const held = Promise.withResolvers<NotificationFeed>()
        feedMock.mockReturnValue(held.promise)

        emit('notifications_changed', { unread: 99 })
        await flushPromises()
        expect(store.badge).toBe(0)

        held.resolve(
            feed({
                data: [series('h1', { unread: true, alerted_at: 'A1' })],
                unread: 2,
            })
        )
        await flushPromises()
        expect(store.badge).toBe(2)

        // A later alert stays unread after the earlier one was marked seen.
        seenMock.mockResolvedValue({ unread: 0 })
        await store.markSeen(store.items)
        expect(store.items[0]!.unread).toBe(false)
        expect(seenMock).toHaveBeenCalledWith([{ id: 'h1', alerted_at: 'A1' }])

        feedMock.mockResolvedValue(
            feed({
                data: [series('h1', { unread: true, alerted_at: 'A2' })],
                unread: 1,
            })
        )
        emit('notifications_changed', { unread: 1 })
        await flushPromises()
        expect(store.items[0]!.unread).toBe(true)
        expect(store.badge).toBe(1)
    })
})

describe('refresh', () => {
    it('refetches what is loaded and replaces it with the response', async () => {
        const store = useNotificationStore()
        feedMock.mockResolvedValue(
            feed({ data: [series('h1'), series('h2'), series('h3')], next: 'c1' })
        )
        await startSync()
        feedMock.mockResolvedValue(feed({ data: [series('h4'), series('h5')], next: null }))
        await store.loadMore()
        expect(ids(store.history)).toEqual(['h1', 'h2', 'h3', 'h4', 'h5'])

        // A new item, a changed one from the second page moved up, and one dismissed elsewhere.
        feedMock.mockResolvedValue(
            feed({
                data: [series('hn'), series('h5'), series('h1'), series('h3'), series('h4')],
                next: 'c2',
            })
        )
        emit('notifications_changed', { unread: 0 })
        await flushPromises()

        expect(feedMock).toHaveBeenLastCalledWith({ limit: 30 })
        expect(ids(store.history)).toEqual(['hn', 'h5', 'h1', 'h3', 'h4'])
        expect(store.next).toBe('c2')

        // It asks for at most 100 and drops the tail it didn’t refetch.
        feedMock.mockResolvedValue(feed({ data: many(100), next: 'c1' }))
        emit('notifications_changed', { unread: 0 })
        await flushPromises()
        feedMock.mockResolvedValue(feed({ data: many(20, 'm'), next: null }))
        await store.loadMore()
        expect(store.history).toHaveLength(120)

        feedMock.mockResolvedValue(feed({ data: many(100), next: 'c2' }))
        emit('notifications_changed', { unread: 0 })
        await flushPromises()
        expect(feedMock).toHaveBeenLastCalledWith({ limit: 100 })
        expect(store.history).toHaveLength(100)
        expect(store.next).toBe('c2')

        // It empties when everything was cleared elsewhere.
        feedMock.mockResolvedValue(feed())
        emit('notifications_changed', { unread: 0 })
        await flushPromises()
        expect(store.history).toEqual([])
        expect(store.next).toBeNull()
    })

    it('keeps dismissed and cleared items out of a refresh already in flight', async () => {
        const store = useNotificationStore()
        feedMock.mockResolvedValue(feed({ data: many(3), next: null }))
        await startSync()

        const inflight = Promise.withResolvers<NotificationFeed>()
        feedMock.mockReturnValueOnce(inflight.promise)
        const refreshing = store.refresh()
        const dismissed = Promise.withResolvers<{ unread: number }>()
        dismissMock.mockReturnValueOnce(dismissed.promise)
        void store.dismiss('h1')
        inflight.resolve(feed({ data: many(3), next: null }))
        await refreshing
        expect(ids(store.history)).toEqual(['h0', 'h2'])

        // A failed delete drops the hold and refetches.
        dismissed.reject(new Error('boom'))
        await flushPromises()
        expect(ids(store.history)).toEqual(['h0', 'h1', 'h2'])

        const again = Promise.withResolvers<NotificationFeed>()
        feedMock.mockReturnValueOnce(again.promise)
        const second = store.refresh()
        clearMock.mockResolvedValueOnce({ unread: 0 })
        void store.clear()
        again.resolve(feed({ data: many(3), next: null }))
        await second
        expect(store.history).toEqual([])
    })

    it('runs requests in order, each reading its parameters when it starts', async () => {
        const store = useNotificationStore()
        feedMock.mockResolvedValue(feed({ data: many(2), next: 'c0' }))
        await startSync()

        const first = Promise.withResolvers<NotificationFeed>()
        const second = Promise.withResolvers<NotificationFeed>()
        feedMock.mockReturnValueOnce(first.promise)
        feedMock.mockResolvedValueOnce(feed({ data: many(5, 'm'), next: null }))
        feedMock.mockReturnValueOnce(second.promise)
        emit('notifications_changed', { unread: 0 })
        await flushPromises()
        const more = store.loadMore()
        emit('notifications_changed', { unread: 0 })
        await flushPromises()
        expect(feedMock).toHaveBeenCalledTimes(2)

        first.resolve(feed({ data: many(40), next: 'c1' }))
        await more
        // loadMore went after the first refresh, with the cursor that response brought.
        expect(feedMock).toHaveBeenNthCalledWith(3, { cursor: 'c1' })
        await flushPromises()
        // The second refresh asked for as many items as were then loaded.
        expect(feedMock).toHaveBeenNthCalledWith(4, { limit: 45 })
        second.resolve(feed({ data: [...many(40), ...many(5, 'm')] }))
        await flushPromises()
        expect(new Set(ids(store.history)).size).toBe(store.history.length)
    })
})
