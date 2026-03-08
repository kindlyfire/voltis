import type { UseQueryOptions } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter, type Ref, type UnwrapRef } from 'vue'

export type QueryOptions<T> = Omit<UnwrapRef<UseQueryOptions<T>>, 'queryKey' | 'queryFn'>

/** Used to conditionally enable queries depending on if their input data is
 * available or not. */
export function isEnabled(
    values: Array<MaybeRefOrGetter<any>> | MaybeRefOrGetter<any>
): Ref<boolean> {
    return computed(() => {
        const vals = Array.isArray(values) ? values : [values]
        return vals.every(v => toValue(v) != null)
    })
}
