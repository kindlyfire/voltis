import { useQueries, useQueryClient } from '@tanstack/vue-query'
import { useDebounceFn } from '@vueuse/core'
import { computed, onScopeDispose, shallowRef, watch, type Ref } from 'vue'
import { contentApi, contentWindowKey, PAGE_SIZE } from '@/utils/api/content'
import type { Content, ContentListParams } from '@/utils/api/types'

/**
 * Loads the list as fixed-size pages around the viewport, one query per page, sized by the
 * buckets' total.
 *
 * @param range item indices of the viewport rows plus overscan
 * @param paused freezes the loaded pages while the rail is dragged
 * @param pinned index of the focused item, whose page stays loaded
 */
export function useContentWindow(
    params: Ref<ContentListParams>,
    range: Ref<{ start: number; end: number }>,
    paused: Ref<boolean>,
    pinned: Ref<number | null>
) {
    const qBuckets = contentApi.useBuckets(params)
    const total = computed(() => qBuckets.data.value?.total ?? 0)

    // Page 0 until the total is known, so it loads alongside the buckets.
    const bounds = computed(() => {
        const last = Math.max(0, Math.ceil(total.value / PAGE_SIZE) - 1)
        return [
            Math.min(last, Math.max(0, Math.floor(range.value.start / PAGE_SIZE) - 1)),
            Math.min(last, Math.floor(range.value.end / PAGE_SIZE) + 1),
        ] as const
    })
    const applied = shallowRef(bounds.value)
    const apply = () => {
        if (!paused.value) applied.value = bounds.value
    }
    // A steady fling still loads pages every 250ms.
    const debounced = useDebounceFn(apply, 80, { maxWait: 250 })
    watch(() => bounds.value.join(':'), debounced)
    onScopeDispose(debounced.cancel)
    watch(paused, apply)

    const pages = computed(() => {
        const [first, last] = applied.value
        const out: number[] = []
        for (let n = first; n <= last; n++) out.push(n)
        const pin = pinned.value === null ? null : Math.floor(pinned.value / PAGE_SIZE)
        if (pin !== null && (pin < first || pin > last)) out.push(pin)
        return out
    })

    const results = useQueries({
        queries: computed(() =>
            pages.value.map(n => ({
                queryKey: [...contentWindowKey(params.value), 'page', n],
                // From the key, TanStack's snapshot: `params` may change before a retry.
                queryFn: ({ queryKey }: { queryKey: readonly unknown[] }) =>
                    contentApi.listPage(queryKey[4] as ContentListParams, n),
                // Tagged with the page, since results trail `pages` until the observer updates.
                select: (res: { data: Content[] }) => ({ n, items: res.data }),
            }))
        ),
        shallow: true,
    })

    const loaded = computed(
        () => new Map(results.value.flatMap(r => (r.data ? [[r.data.n, r.data.items]] : [])))
    )
    // Pages outside the debounced window still show from the cache, e.g. where back navigation
    // restores the scroll position: the debounce only saves requests.
    const client = useQueryClient()
    const itemAt = (index: number): Content | undefined => {
        const n = Math.floor(index / PAGE_SIZE)
        const items =
            loaded.value.get(n) ??
            client.getQueryData<{ data: Content[] }>([...contentWindowKey(params.value), 'page', n])
                ?.data
        return items?.[index % PAGE_SIZE]
    }

    const errorValue = computed(
        () => qBuckets.error.value ?? results.value.find(r => r.isError)?.error ?? null
    )

    return {
        qBuckets,
        error: { isError: computed(() => errorValue.value !== null), error: errorValue },
        total,
        itemAt,
        pending: computed(() => qBuckets.isPending.value || results.value.some(r => r.isPending)),
    }
}
