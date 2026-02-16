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

function onMessage(event: MessageEvent) {
    try {
        const msg = JSON.parse(event.data)
        const handlers = listeners.get(msg.type)
        if (handlers) {
            for (const h of handlers) h(msg)
        }
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
    }
    socket.onmessage = onMessage
    socket.onclose = scheduleReconnect
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

// --- Composables ---

import { onUnmounted } from 'vue'

export interface ScanStatusItem {
    library_id: string
    library_name: string
    status: 'running' | 'queued' | 'done'
    summary?: { to_add: number; to_update: number; to_remove: number; unchanged: number }
    progress?: { total: number; processed: number }
}

export function useWsOnScanStatus(cb: (msg: { queue: ScanStatusItem[] }) => void) {
    const unsub = ws.on('scan_status', cb)
    onUnmounted(unsub)
    return unsub
}
