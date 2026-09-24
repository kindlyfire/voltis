import { ref } from 'vue'

export interface ToastOptions {
    message: string
    tone?: 'success' | 'danger' | 'info'
    action?: {
        label: string
        /** Describes how to do the same without the toast. */ altText: string
        onClick: () => void
    }
    /** Milliseconds. */
    duration?: number
}

export interface Toast extends ToastOptions {
    id: number
    open: boolean
}

/** Waiting toasts beyond this drop the oldest waiting one. */
const MAX_QUEUED = 3
/** Leaves time for the exit animation before the next toast shows. */
const EXIT_MS = 200

let nextId = 0

/** The queue. Only the first toast is shown; AToastRegion renders it. */
export const toasts = ref<Toast[]>([])

export function dismissToast(id: number) {
    const index = toasts.value.findIndex(t => t.id === id)
    if (index < 0) return
    if (index > 0) {
        toasts.value.splice(index, 1)
        return
    }
    const toast = toasts.value[0]!
    if (!toast.open) return
    toast.open = false
    setTimeout(() => {
        toasts.value = toasts.value.filter(t => t.id !== id)
    }, EXIT_MS)
}

function show(options: ToastOptions) {
    const id = nextId++
    toasts.value.push({ ...options, id, open: true })
    if (toasts.value.length > MAX_QUEUED + 1) toasts.value.splice(1, 1)
    return { dismiss: () => dismissToast(id) }
}

export function useToast() {
    return { show }
}
