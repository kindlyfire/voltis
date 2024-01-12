export type MaybePromise<T> = T | Promise<T>

export function formatDate(d: Date) {
	return new Intl.DateTimeFormat('en-US', {
		year: 'numeric',
		month: 'long',
		day: 'numeric'
	}).format(d)
}
