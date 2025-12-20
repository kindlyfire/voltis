import { computed, toValue, type MaybeRefOrGetter, type Ref } from 'vue'

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
