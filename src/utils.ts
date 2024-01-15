export type MaybePromise<T> = T | Promise<T>

export function formatDate(d: Date) {
	return new Intl.DateTimeFormat('en-US', {
		year: 'numeric',
		month: 'long',
		day: 'numeric'
	}).format(d)
}

type OptionalPropertyNames<T> = {
	[K in keyof T]-?: {} extends { [P in K]: T[K] } ? K : never
}[keyof T]
type SpreadProperties<L, R, K extends keyof L & keyof R> = {
	[P in K]: L[P] | Exclude<R[P], undefined>
}
export type Id<T> = T extends infer U ? { [K in keyof U]: U[K] } : never
export type SpreadTwo<L, R> = Id<
	Pick<L, Exclude<keyof L, keyof R>> &
		Pick<R, Exclude<keyof R, OptionalPropertyNames<R>>> &
		Pick<R, Exclude<OptionalPropertyNames<R>, keyof L>> &
		SpreadProperties<L, R, OptionalPropertyNames<R> & keyof L>
>
export type Spread<A extends readonly [...any]> = A extends [
	infer L,
	...infer R
]
	? SpreadTwo<L, Spread<R>>
	: unknown
