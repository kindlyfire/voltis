import { keepPreviousData } from '@tanstack/vue-query'
import { computed, ref, toValue, type MaybeRefOrGetter } from 'vue'
import { contentApi } from '@/utils/api/content'
import type { Content } from '@/utils/api/types'

export interface Siblings {
    /**
     * `ready` once the current series' list holds the current volume (always, for standalone
     * content); `loading` while that list, or the content itself, is still to come; `error` when
     * the list failed or came without the current volume, which a scan can move or remove.
     */
    status: 'loading' | 'ready' | 'error'
    /** The series' volumes in order; none for standalone content. */
    items: Content[]
    /** The current one's place among them, or -1. */
    index: number
    prev: Content | null
    next: Content | null
    /** Reads the content again, for its current series, and that series' list. */
    retry(): void
}

type Placed = { id: string; parent_id: string | null }

/**
 * The volumes around `content`, from the readers' one sibling query. Only this series' own list
 * counts: one kept from another series while this one's loads leaves them loading, so nothing
 * mistakes it for the end of the series or navigates by it.
 */
export function useSiblings(
    content: MaybeRefOrGetter<Placed | null>,
    enabled: MaybeRefOrGetter<boolean> = true
) {
    // The content as a retry read it again, which supersedes the reader's copy.
    const reread = ref<Placed | null>(null)
    const rereading = ref(false)
    const current = computed(() => {
        const c = toValue(content)
        return c && reread.value?.id === c.id ? reread.value : c
    })
    const query = contentApi.useList(
        () => {
            const parent = current.value?.parent_id
            if (parent && toValue(enabled)) {
                return { parent_id: parent, sort: 'order', sort_order: 'asc' }
            }
        },
        { placeholderData: keepPreviousData }
    )

    async function retry() {
        const c = current.value
        if (!c || rereading.value) return
        rereading.value = true
        try {
            const { id, parent_id } = await contentApi.get(c.id)
            reread.value = { id, parent_id }
        } catch {
            // The list's refetch below reports the failure.
        } finally {
            rereading.value = false
        }
        void query.refetch()
    }

    return computed<Siblings>(() => {
        const of = (status: Siblings['status'], items: Content[] = [], index = -1) => ({
            status,
            items,
            index,
            prev: index > 0 ? items[index - 1]! : null,
            next: index >= 0 ? (items[index + 1] ?? null) : null,
            retry,
        })
        const c = current.value
        if (!c) return of('loading')
        if (!c.parent_id) return of('ready')
        const items = query.isPlaceholderData.value ? undefined : query.data.value?.data
        const index = items?.findIndex(item => item.id === c.id) ?? -1
        if (items && index >= 0) return of('ready', items, index)
        if (rereading.value || query.isFetching.value) return of('loading')
        return of(items || query.isError.value ? 'error' : 'loading')
    })
}
