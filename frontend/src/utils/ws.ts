import { onUnmounted, ref, watch, type Ref } from 'vue'
import { API_URL } from './fetch'

type Handler = (message: any) => void

const listeners = new Map<string, Set<Handler>>()
let socket: WebSocket | null = null
let reconnectDelay = 1000

function getWsUrl() {
    if (API_URL.startsWith('http')) {
        return API_URL.replace(/^http/, 'ws') + '/ws'
    }
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${location.host}${API_URL}/ws`
}

function dispatch(type: string, msg: any) {
    const handlers = listeners.get(type)
    if (handlers) {
        for (const h of handlers) h(msg)
    }
}

function onMessage(event: MessageEvent) {
    try {
        const msg = JSON.parse(event.data)
        dispatch(msg.type, msg)
    } catch (e) {
        console.error('Failed to parse WebSocket message', e)
    }
}

function scheduleReconnect() {
    setTimeout(() => {
        reconnectDelay = Math.min(reconnectDelay * 2, 30_000)
        connect()
    }, reconnectDelay)
}

function connect() {
    if (socket?.readyState === WebSocket.OPEN || socket?.readyState === WebSocket.CONNECTING) {
        return
    }
    socket = new WebSocket(getWsUrl())
    socket.onopen = () => {
        reconnectDelay = 1000
        dispatch('$open', { type: '$open' })
    }
    socket.onmessage = onMessage
    socket.onclose = () => {
        dispatch('$close', { type: '$close' })
        scheduleReconnect()
    }
    socket.onerror = () => socket?.close()
}

function send(data: unknown) {
    socket?.send(JSON.stringify(data))
}

function on(type: string, handler: Handler): () => void {
    let set = listeners.get(type)
    if (!set) {
        set = new Set()
        listeners.set(type, set)
    }
    set.add(handler)
    return () => set!.delete(handler)
}

export const ws = { connect, send, on }

export interface ScanTask {
    taskId: string | null
    libraryId: string
    status: 'queued' | 'running' | 'completed' | 'failed'
    output: { to_add: number; to_update: number; to_remove: number; unchanged: number } | null
    progress: { total: number; processed: number } | null
}

interface TaskUpdateMsg {
    type: 'task_update'
    task: {
        id: string
        status?: number
        input?: { library_id?: string }
        output?: { to_add: number; to_update: number; to_remove: number; unchanged: number }
        logs?: string
    }
    progress?: { total: number; processed: number }
}

interface ScanQueueUpdateMsg {
    type: 'scan_queue_update'
    library_ids: string[]
}

const STATUS_MAP: Record<number, ScanTask['status']> = {
    1: 'running',
    2: 'completed',
    3: 'failed',
}

export function useScanTracker(isAdmin: Ref<boolean>): {
    scans: Ref<ScanTask[]>
    clear: () => void
} {
    const scans = ref<ScanTask[]>([])

    function findByTask(taskId: string): ScanTask | undefined {
        return scans.value.find(s => s.taskId === taskId)
    }

    function onTaskUpdate(msg: TaskUpdateMsg) {
        const libraryId = msg.task.input?.library_id
        let scan = findByTask(msg.task.id)

        if (!scan) {
            if (!libraryId) return
            scan = {
                taskId: msg.task.id,
                libraryId,
                status: 'running',
                output: null,
                progress: null,
            }
            scans.value.push(scan)
        }

        scan.taskId = msg.task.id
        if (msg.task.status != null && STATUS_MAP[msg.task.status]) {
            scan.status = STATUS_MAP[msg.task.status]!
        }
        if (msg.task.output != null) {
            scan.output = msg.task.output
        }
        if (msg.progress != null) {
            scan.progress = msg.progress
        }
    }

    function clear() {
        scans.value = []
    }

    let unsubs: (() => void)[] = []

    function unsubscribe() {
        for (const unsub of unsubs) unsub()
        unsubs = []
    }

    watch(
        isAdmin,
        admin => {
            if (admin) {
                unsubs = [ws.on('task_update', onTaskUpdate), ws.on('$close', clear)]
            } else {
                unsubscribe()
                clear()
            }
        },
        { immediate: true }
    )

    onUnmounted(unsubscribe)

    return { scans, clear }
}
