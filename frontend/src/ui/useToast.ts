import { computed, ref } from 'vue'

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

/** Toasts shown at once. The rest wait. */
const MAX_VISIBLE = 3
/** Waiting toasts beyond this drop the oldest waiting one. */
const MAX_WAITING = 3
/** A closed toast keeps its slot for its exit animation. */
const EXIT_MS = 200

let nextId = 0

/** Visible toasts first (oldest first), then the waiting ones. */
export const toasts = ref<Toast[]>([])

/** The toasts AToastRegion renders, closing ones included. */
export const visibleToasts = computed(() => toasts.value.slice(0, MAX_VISIBLE))

export function dismissToast(id: number) {
    const index = toasts.value.findIndex(t => t.id === id)
    if (index < 0) return
    if (index >= MAX_VISIBLE) {
        toasts.value.splice(index, 1)
        return
    }
    const toast = toasts.value[index]!
    if (!toast.open) return
    toast.open = false
    setTimeout(() => {
        toasts.value = toasts.value.filter(t => t.id !== id)
    }, EXIT_MS)
}

function show(options: ToastOptions) {
    const id = nextId++
    toasts.value.push({ ...options, id, open: true })
    if (toasts.value.length > MAX_VISIBLE + MAX_WAITING) toasts.value.splice(MAX_VISIBLE, 1)
    return { dismiss: () => dismissToast(id) }
}

export function useToast() {
    return { show }
}
