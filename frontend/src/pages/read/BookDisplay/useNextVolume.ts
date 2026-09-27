import { computed, type MaybeRefOrGetter, toValue } from 'vue'
import { contentApi } from '@/utils/api/content'
import type { Content } from '@/utils/api/types'

/** The readable book with the smallest `order` after the current one. Array
 * position means nothing: `order` can be null before a series flush, and the
 * list has no tie-breaker. */
export function nextVolume(current: Content, siblings: Content[]): Content | null {
    const from = current.order
    if (from == null) return null
    let best: Content | null = null
    for (const item of siblings) {
        if (item.type !== 'book' || !item.valid || item.order == null || item.order <= from)
            continue
        if (!best || item.order < best.order!) best = item
    }
    return best
}

/** Same query as the comic reader's sibling list, so they share a cache. */
export function useNextVolume(
    content: MaybeRefOrGetter<Content | null>,
    enabled: MaybeRefOrGetter<boolean>
) {
    const query = contentApi.useList(() => {
        const current = toValue(content)
        if (current?.parent_id && toValue(enabled)) {
            return { parent_id: current.parent_id, sort: 'order', sort_order: 'asc' }
        }
    })
    return computed(() => {
        const current = toValue(content)
        const siblings = query.data.value?.data
        return current && siblings && toValue(enabled) ? nextVolume(current, siblings) : null
    })
}
