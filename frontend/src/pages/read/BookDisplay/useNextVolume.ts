import { computed, type MaybeRefOrGetter } from 'vue'
import type { Content } from '@/utils/api/types'
import { useSiblings, type Siblings } from '../useSiblings'

/** The first readable book after the current volume, once the series' siblings are known. */
export function nextVolume({ status, items, index }: Siblings): Content | null {
    if (status !== 'ready' || index < 0) return null
    return items.slice(index + 1).find(item => item.type === 'book' && item.valid) ?? null
}

/** The next volume, with the siblings' state for the end of the book. */
export function useNextVolume(
    content: MaybeRefOrGetter<Content | null>,
    enabled: MaybeRefOrGetter<boolean>
) {
    const siblings = useSiblings(content, enabled)
    const next = computed(() => nextVolume(siblings.value))
    return { siblings, next }
}
