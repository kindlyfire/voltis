export function unixNow() {
	return Math.floor(Date.now() / 1000)
}

export function filterTruthyArrayValues<T>(
	arr: (T | undefined | null | false)[]
) {
	return arr.filter(v => !(v == null || v === false)) as T[]
}

export function pickKeys<T extends object, K extends keyof T>(
	obj: T,
	keys: K[]
) {
	const out = {} as Pick<T, K>
	for (const key of keys) {
		out[key] = obj[key]
	}
	return out
}

export function omitKeys<T extends object, K extends keyof T>(
	obj: T,
	keys: K[]
) {
	const out = { ...obj }
	for (const key of keys) {
		delete out[key]
	}
	return out as Omit<T, K>
}

export function newUnpackedPromise<T = void>() {
	let resolve!: (value: T) => void
	let reject!: (reason?: any) => void
	const promise = new Promise<T>((res, rej) => {
		resolve = res
		reject = rej
	})
	return { promise, resolve, reject }
}

export async function promiseAllSettled2<T>(
	promises: Promise<T>[]
): Promise<[fulfilled: T[], rejected: any[]]> {
	const settled = await Promise.allSettled(promises)
	const fulfilled: T[] = []
	const rejected: any[] = []
	for (const p of settled) {
		if (p.status === 'fulfilled') {
			fulfilled.push(p.value)
		} else {
			rejected.push(p.reason)
		}
	}
	return [fulfilled, rejected] as const
}
