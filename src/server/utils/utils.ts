import { nanoid } from 'nanoid'

// NOTE: Do not change ID length. Some things depend on it.
export function createId(prefix: 'c' | 'i' | 'u' | 's' | 'l'): string {
	return prefix + nanoid(12)
}

export function unixNow() {
	return Math.floor(Date.now() / 1000)
}

export function filterTruthyArrayValues<T>(
	arr: (T | undefined | null | false)[]
) {
	return arr.filter(v => !(v == null || v === false)) as T[]
}

export function pickKeys<T extends Record<string, unknown>, K extends keyof T>(
	obj: T,
	keys: K[]
) {
	const out = {} as Pick<T, K>
	for (const key of keys) {
		out[key] = obj[key]
	}
	return out
}

export function omitKeys<T extends Record<string, unknown>, K extends keyof T>(
	obj: T,
	keys: K[]
) {
	const out = { ...obj }
	for (const key of keys) {
		delete out[key]
	}
	return out
}

export function newUnpackedPromise<T = void>() {
	let resolve!: (value: T) => void
	let reject!: (reason?: any) => void
	const promise = new Promise((res, rej) => {
		resolve = res
		reject = rej
	})
	return { promise, resolve, reject }
}
