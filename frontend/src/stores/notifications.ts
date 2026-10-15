import { defineStore } from 'pinia'
import { computed, reactive, ref, watch as vueWatch } from 'vue'
import { notificationsApi } from '@/utils/api/notifications'
import { type Notification, TaskStatus, type TaskSnapshot } from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'
import { ws } from '@/utils/ws'

const MAX_LIMIT = 100
const SEEN_CHUNK = 500

/**
 * The user's feed as the server last said it. `serverUnread` and each item's `unread` change only
 * when a response is applied, and every request that returns them runs one at a time, so
 * responses apply in send order and each reflects the writes before it.
 */
export const useNotificationStore = defineStore('notifications', () => {
    const active = ref<Notification[]>([])
    const needsInput = ref<Notification[]>([])
    const history = ref<Notification[]>([])
    const next = ref<string | null>(null)
    const serverUnread = ref(0)
    const loaded = ref(false)

    // Scans whose results the user is looking at: ref-counted by the open scan modals.
    const watched = reactive(new Map<string, number>())
    // Watched scans that ended and whose item still has to be marked seen.
    const pendingSeen = reactive(new Map<string, 'waiting' | 'sending'>())

    // Deleted optimistically and not yet settled, so a refresh already in flight can't bring them back.
    const pendingDeleted = new Set<string>()
    // Clear All: items last updated at or before this are gone, until the request settles.
    let clearedThrough: number | null = null

    let generation = 0
    let chain: Promise<void> = Promise.resolve()
    let queuedRefresh: Promise<void> | null = null

    // Watched scans' items the loaded pages don't hold, fetched by task.
    const extra = ref<Notification[]>([])

    const items = computed(() => {
        const loadedItems = [...active.value, ...needsInput.value, ...history.value]
        const have = new Set(loadedItems.map(i => i.id))
        return [...loadedItems, ...extra.value.filter(i => !have.has(i.id))]
    })

    // A watched scan's alert is never counted: not from the feed that first shows it, and not
    // until the seen response that removes it from the server's count. Matched by task id, loaded
    // page or not.
    const badge = computed(() => {
        const hidden = items.value.filter(
            i => i.unread && i.task_id && (watched.has(i.task_id) || pendingSeen.has(i.task_id))
        ).length
        return Math.max(0, serverUnread.value - hidden)
    })

    /** Runs `run` after the requests before it, unless a reset came since. */
    function enqueue(run: () => Promise<void>): Promise<void> {
        const gen = generation
        const result = chain.then(async () => {
            if (gen !== generation) return
            try {
                await run()
            } catch (err) {
                console.error('Notification request failed', err)
            }
        })
        chain = result
        return result
    }

    function visible(list: Notification[]): Notification[] {
        return list.filter(
            i =>
                !pendingDeleted.has(i.id) &&
                (clearedThrough === null || Date.parse(i.updated_at) > clearedThrough)
        )
    }

    function byTask(taskId: string): Notification | undefined {
        return items.value.find(i => i.task_id === taskId)
    }

    function hasOutcome(item: Notification): boolean {
        return item.kind === 'scan' && !!item.data.outcome
    }

    function refresh(): Promise<void> {
        if (queuedRefresh) return queuedRefresh
        const gen = generation
        const queued = enqueue(async () => {
            queuedRefresh = null
            // Covers what loadMore loaded, up to the route's maximum.
            const limit = Math.min(MAX_LIMIT, Math.max(30, history.value.length))
            const res = await notificationsApi.feed({ limit })
            if (gen !== generation) return
            // Watched scans missing from the pages, so they still count as watched and get seen.
            res.active = visible(res.active)
            res.needs_input = visible(res.needs_input)
            res.data = visible(res.data)
            const have = new Set(
                [...res.active, ...res.needs_input, ...res.data].map(i => i.task_id)
            )
            const missing = [...new Set([...watched.keys(), ...pendingSeen.keys()])].filter(
                id => !have.has(id)
            )
            const fetched = (
                await Promise.all(missing.map(taskId => notificationsApi.feed({ taskId })))
            ).flatMap(r => visible(r.data))
            if (gen !== generation) return
            extra.value = fetched
            active.value = res.active
            needsInput.value = res.needs_input
            history.value = res.data
            next.value = res.next
            serverUnread.value = res.unread
            loaded.value = true
            settleWatched()
        })
        queuedRefresh = queued
        return queued
    }

    /** Finds the watched scans that ended, and sends their items' seen marks. */
    function settleWatched(): void {
        for (const item of items.value) {
            const id = item.task_id
            if (item.unread && id && hasOutcome(item) && watched.has(id) && !pendingSeen.has(id)) {
                pendingSeen.set(id, 'waiting')
            }
        }
        for (const [id, state] of [...pendingSeen]) {
            const item = byTask(id)
            if (!item) pendingSeen.delete(id)
            else if (!hasOutcome(item)) continue
            else if (!item.unread) pendingSeen.delete(id)
            else if (state === 'waiting') {
                pendingSeen.set(id, 'sending')
                void markSeen([item])
            }
        }
    }

    /** Marks the unread items seen, at the version this client has. */
    function markSeen(list: Notification[]): Promise<void> {
        const unread = list.filter(i => i.unread && i.alerted_at)
        const chunks: Notification[][] = []
        for (let i = 0; i < unread.length; i += SEEN_CHUNK)
            chunks.push(unread.slice(i, i + SEEN_CHUNK))
        const gen = generation
        return Promise.all(
            chunks.map(chunk =>
                enqueue(async () => {
                    try {
                        const res = await notificationsApi.seen(
                            chunk.map(i => ({ id: i.id, alerted_at: i.alerted_at! }))
                        )
                        if (gen !== generation) return
                        for (const sent of chunk) {
                            const cur = items.value.find(i => i.id === sent.id)
                            if (cur && cur.alerted_at === sent.alerted_at) cur.unread = false
                            if (sent.task_id) pendingSeen.delete(sent.task_id)
                        }
                        serverUnread.value = res.unread
                    } catch (err) {
                        // The next refresh sends them again.
                        if (gen === generation) {
                            for (const { task_id: id } of chunk) {
                                if (id && pendingSeen.get(id) === 'sending')
                                    pendingSeen.set(id, 'waiting')
                            }
                        }
                        throw err
                    }
                })
            )
        ).then(() => {})
    }

    function loadMore(): Promise<void> {
        const gen = generation
        return enqueue(async () => {
            const cursor = next.value
            if (!cursor) return
            const res = await notificationsApi.feed({ cursor })
            if (gen !== generation) return
            const have = new Set(history.value.map(i => i.id))
            const fresh = visible(res.data).filter(i => !have.has(i.id))
            history.value = [...history.value, ...fresh]
            next.value = res.next
            serverUnread.value = res.unread
            void markSeen(fresh)
        })
    }

    function dismiss(id: string): Promise<void> {
        for (const list of [active, needsInput, history]) {
            list.value = list.value.filter(i => i.id !== id)
        }
        pendingDeleted.add(id)
        const gen = generation
        return enqueue(async () => {
            try {
                const res = await notificationsApi.dismiss(id)
                if (gen === generation) serverUnread.value = res.unread
                pendingDeleted.delete(id)
            } catch (err) {
                pendingDeleted.delete(id)
                void refresh()
                throw err
            }
        })
    }

    function clear(): Promise<void> {
        const through = Math.max(
            clearedThrough ?? 0,
            ...items.value.map(i => Date.parse(i.updated_at))
        )
        clearedThrough = through
        active.value = needsInput.value = history.value = []
        next.value = null
        const gen = generation
        return enqueue(async () => {
            try {
                const res = await notificationsApi.clear()
                if (gen === generation) serverUnread.value = res.unread
                if (clearedThrough === through) clearedThrough = null
            } catch (err) {
                if (clearedThrough === through) clearedThrough = null
                void refresh()
                throw err
            }
        })
    }

    /** Hides the scans' results from the badge while their details are open. */
    function watch(ids: string[]): () => void {
        for (const id of ids) watched.set(id, (watched.get(id) ?? 0) + 1)
        // A scan that ended before the watch began has no event left to settle it.
        settleWatched()
        void refresh()
        let stopped = false
        return () => {
            if (stopped) return
            stopped = true
            for (const id of ids) {
                const n = (watched.get(id) ?? 1) - 1
                if (n > 0) watched.set(id, n)
                else watched.delete(id)
            }
        }
    }

    function onTaskUpdate(task: TaskSnapshot): void {
        if (!watched.has(task.id) || task.status < TaskStatus.COMPLETED) return
        if (!pendingSeen.has(task.id)) pendingSeen.set(task.id, 'waiting')
        void refresh()
    }

    function reset(): void {
        generation++
        chain = Promise.resolve()
        queuedRefresh = null
        active.value = needsInput.value = history.value = extra.value = []
        next.value = null
        serverUnread.value = 0
        loaded.value = false
        watched.clear()
        pendingSeen.clear()
        pendingDeleted.clear()
        clearedThrough = null
    }

    return {
        active,
        needsInput,
        history,
        next,
        loaded,
        badge,
        items,
        byTask,
        refresh,
        markSeen,
        loadMore,
        dismiss,
        clear,
        watch,
        onTaskUpdate,
        reset,
    }
})

export function useNotificationSync(): void {
    const store = useNotificationStore()
    const qMe = usersApi.useMe()
    const userId = computed(() => qMe.data.value?.id)

    vueWatch(
        userId,
        (id, _, onCleanup) => {
            store.reset()
            if (!id) return
            const unsubs = [
                ws.on('notifications_changed', () => void store.refresh()),
                ws.on('$open', () => void store.refresh()),
                ws.on('task_update', (msg: { task: TaskSnapshot }) => store.onTaskUpdate(msg.task)),
            ]
            onCleanup(() => unsubs.forEach(unsub => unsub()))
            void store.refresh()
        },
        { immediate: true }
    )
}
