import { hashKey } from '@tanstack/vue-query'
import { onScopeDispose, ref, watch, type Ref } from 'vue'
import { useToast } from '@/ui/useToast'
import { contentApi, invalidateContentWindow } from '@/utils/api/content'
import type { ContentListParams } from '@/utils/api/types'

/**
 * Selection by explicit ids. Shift-click adds the index range from the last clicked item, fetched
 * from the server, since it can span pages that were never loaded.
 */
export function useGridSelection(params: Ref<ContentListParams>) {
    const toast = useToast()
    const selectMode = ref(false)
    const ids = ref(new Set<string>())
    let anchor: { id: string; index: number } | null = null
    // Bumped by clear(), which drops the operations queued or in flight before it.
    let generation = 0
    // Operations apply in click order, so a toggle queued behind a pending range is not undone
    // when the range lands.
    let chain = Promise.resolve()

    function clear() {
        generation++
        chain = Promise.resolve() // new operations don't wait for dropped ones
        anchor = null
        ids.value.clear()
    }

    function setSelectMode(on: boolean) {
        selectMode.value = on
        if (!on) clear()
    }

    // Ids hidden by the new filter would be acted on unseen, and the anchor is an index in the
    // old order.
    watch(() => hashKey([params.value]), clear)
    onScopeDispose(clear)

    function flip(id: string) {
        if (!ids.value.delete(id)) ids.value.add(id)
    }

    function toggle(id: string, index: number, shiftKey: boolean): Promise<void> {
        const gen = generation
        const from = shiftKey ? anchor : null
        anchor = { id, index }
        if (!from) return enqueue(gen, () => flip(id))

        const [lo, hi] = from.index <= index ? [from, anchor] : [anchor, from]
        // Requested now, applied in click order.
        const request = contentApi.ids(params.value, lo.index, hi.index - lo.index + 1).then(
            res => res.ids,
            () => null
        )
        return enqueue(gen, async () => {
            const range = await request
            if (gen !== generation) return
            if (!range) {
                toast.show({ message: "Couldn't select the range", tone: 'danger' })
            } else if (range[0] === lo.id && range.at(-1) === hi.id) {
                for (const rangeId of range) ids.value.add(rangeId)
            } else {
                // The server order drifted from the loaded pages.
                flip(id)
                void invalidateContentWindow()
            }
        })
    }

    function enqueue(gen: number, apply: () => void | Promise<void>): Promise<void> {
        chain = chain.then(() => (gen === generation ? apply() : undefined))
        return chain
    }

    return {
        selectMode,
        setSelectMode,
        ids,
        toggle,
        clear,
    }
}
