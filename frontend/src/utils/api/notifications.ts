import { apiFetch } from '../fetch'
import type { NotificationFeed } from './types'

export const notificationsApi = {
    feed: (params: { cursor?: string; limit?: number; taskId?: string } = {}) => {
        const query = new URLSearchParams()
        if (params.cursor) query.set('cursor', params.cursor)
        if (params.limit) query.set('limit', String(params.limit))
        if (params.taskId) query.set('task_id', params.taskId)
        const qs = query.toString()
        return apiFetch<NotificationFeed>(`/notifications${qs ? `?${qs}` : ''}`)
    },

    /** `alerted_at` is sent back exactly as received. */
    seen: (items: { id: string; alerted_at: string }[]) =>
        apiFetch<{ unread: number }>('/notifications/seen', {
            method: 'POST',
            body: JSON.stringify({ items }),
        }),

    dismiss: (id: string) =>
        apiFetch<{ unread: number }>(`/notifications/${encodeURIComponent(id)}`, {
            method: 'DELETE',
        }),

    clear: () => apiFetch<{ unread: number }>('/notifications', { method: 'DELETE' }),
}
