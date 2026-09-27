import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import { tasksApi } from '@/utils/api/tasks'
import {
    TaskStatus,
    type ScanProgress,
    type ScanRecent,
    type ScanResult,
    type TaskSnapshot,
} from '@/utils/api/types'
import { usersApi } from '@/utils/api/users'
import { ws } from '@/utils/ws'

export function isTerminal(task: TaskSnapshot): boolean {
    return task.status >= TaskStatus.COMPLETED
}

function hasOutput(output: TaskSnapshot['output']): output is ScanResult {
    return !!output && Object.keys(output).length > 0
}

export type ScanCounts = ScanProgress['saved']

export type ScanLead =
    | { state: 'queued' }
    | { state: 'walking'; found: number }
    | { state: 'parsing'; processed: number; total: number }
    | { state: 'saving' }
    | { state: 'done'; outcome: 'completed' | 'failed' | 'cancelled'; counts: ScanCounts }

export interface ScanRow {
    id: string
    libraryId: string
    lead: ScanLead
    recent: ScanRecent[]
}

const noCounts: ScanCounts = { added: 0, updated: 0, removed: 0 }

function scanLead(task: TaskSnapshot): ScanLead {
    const progress = task.progress
    // A failed or cancelled scan still keeps what its earlier flushes committed.
    const saved = progress?.saved ?? noCounts
    if (isTerminal(task)) {
        if (task.status === TaskStatus.COMPLETED) {
            const counts = hasOutput(task.output) ? task.output : saved
            return { state: 'done', outcome: 'completed', counts }
        }
        const outcome = task.status === TaskStatus.CANCELLED ? 'cancelled' : 'failed'
        return { state: 'done', outcome, counts: saved }
    }
    if (task.status === TaskStatus.PENDING || !progress) return { state: 'queued' }
    switch (progress.phase) {
        case 'walking':
            return { state: 'walking', found: progress.found }
        case 'parsing':
            return { state: 'parsing', processed: progress.processed, total: progress.total }
        case 'saving':
            return { state: 'saving' }
        case 'done':
            return { state: 'done', outcome: 'completed', counts: saved }
    }
}

export function scanRow(task: TaskSnapshot): ScanRow {
    return {
        id: task.id,
        libraryId: task.input?.library_id ?? '',
        lead: scanLead(task),
        recent: task.progress?.recent ?? [],
    }
}

function instant(ts: string): [number, number] {
    const frac = /\.(\d+)/.exec(ts)
    return [
        Date.parse(frac ? ts.replace(frac[0], '') : ts),
        frac ? Number(frac[1]!.padEnd(9, '0').slice(0, 9)) : 0,
    ]
}

function isOlder(a: string, b: string): boolean {
    const [as, an] = instant(a)
    const [bs, bn] = instant(b)
    if (Number.isNaN(as) || Number.isNaN(bs)) return false
    return as !== bs ? as < bs : an < bn
}

export const useScanStore = defineStore('scans', () => {
    const tasks = ref<Record<string, TaskSnapshot>>({})
    const logs = ref<Record<string, { text: string; len: number }>>({})
    const dismissed = ref(new Set<string>())
    const inFlight = new Map<string, object>()
    let generation = 0

    function accept(task: TaskSnapshot): void {
        const prev = tasks.value[task.id]
        if (prev && isOlder(task.updated_at, prev.updated_at)) return
        tasks.value[task.id] = {
            ...task,
            progress: task.progress ?? prev?.progress ?? null,
            output: hasOutput(task.output) ? task.output : (prev?.output ?? task.output),
        }
    }

    async function reconcile(ids?: string[]): Promise<void> {
        const gen = generation
        const snapshots = await tasksApi.snapshot(ids)
        if (gen !== generation) return
        for (const task of snapshots) accept(task)
    }

    async function fetchLogs(id: string): Promise<void> {
        if (inFlight.has(id)) return
        const token = {}
        const gen = generation
        inFlight.set(id, token)
        try {
            const res = await tasksApi.logs(id, logs.value[id]?.len ?? 0)
            if (gen !== generation) return
            const held = logs.value[id]
            if (res.offset !== (held?.len ?? 0)) return
            logs.value[id] = { text: (held?.text ?? '') + res.text, len: res.len }
        } finally {
            if (inFlight.get(id) === token) inFlight.delete(id)
        }
    }

    function dismiss(ids: string[]): void {
        for (const id of ids) dismissed.value.add(id)
    }

    function reset(): void {
        generation++
        tasks.value = {}
        logs.value = {}
        dismissed.value.clear()
        inFlight.clear()
    }

    return { tasks, logs, dismissed, accept, reconcile, fetchLogs, dismiss, reset }
})

export function useScanSync(): void {
    const store = useScanStore()
    const qMe = usersApi.useMe()
    const isAdmin = computed(() => !!qMe.data.value?.permissions.includes('ADMIN'))

    watch(
        isAdmin,
        (admin, _, onCleanup) => {
            if (!admin) {
                store.reset()
                return
            }
            const unsubs = [
                ws.on('task_update', (msg: { task: TaskSnapshot }) => store.accept(msg.task)),
                ws.on('$open', () => {
                    void store
                        .reconcile(
                            Object.values(store.tasks)
                                .filter(task => !isTerminal(task))
                                .map(task => task.id)
                        )
                        .catch(() => {})
                }),
            ]
            onCleanup(() => unsubs.forEach(unsub => unsub()))
            void store.reconcile().catch(() => {})
        },
        { immediate: true }
    )
}
