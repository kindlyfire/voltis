import { ref, type Ref } from 'vue'

export interface PageLoaderState {
	readonly index: number
	readonly blobUrl: Ref<string | null>
	readonly loading: Ref<boolean>
	readonly error: Ref<string | null>
	load(): Promise<void>
	dispose(): void
}

export function createPageLoader(index: number, url: string, signal: AbortSignal): PageLoaderState {
	const blobUrl = ref<string | null>(null)
	const loading = ref(false)
	const error = ref<string | null>(null)

	async function load() {
		if (blobUrl.value || loading.value) return

		loading.value = true
		error.value = null

		try {
			const res = await fetch(url, { signal, credentials: 'include' })
			if (!res.ok) throw new Error(`HTTP ${res.status}`)

			const blob = await res.blob()

			// Check if aborted during blob read
			if (signal.aborted) return

			blobUrl.value = URL.createObjectURL(blob)
		} catch (e) {
			if (e instanceof DOMException && e.name === 'AbortError') {
				return // Silently ignore abort
			}
			error.value = e instanceof Error ? e.message : String(e)
		} finally {
			loading.value = false
		}
	}

	function dispose() {
		if (blobUrl.value) {
			URL.revokeObjectURL(blobUrl.value)
			blobUrl.value = null
		}
	}

	return { index, blobUrl, loading, error, load, dispose }
}

/** Returns page indices in preload order: current, then alternating
 * forward/backward. Max two pages backwards. */
export function getPagesInPreloadOrder(pageCount: number, currentPage: number): number[] {
	const result: number[] = []
	for (let i = 0; i < pageCount; i++) {
		const forward = currentPage + i
		const backward = currentPage - i
		if (forward < pageCount) result.push(forward)
		if (i <= 2 && backward !== forward && backward >= 0) result.push(backward)
	}
	return result
}
