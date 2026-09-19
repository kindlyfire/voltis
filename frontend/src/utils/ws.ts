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
    listeners.get(type)?.forEach(handler => handler(msg))
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
