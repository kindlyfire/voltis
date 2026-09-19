import { ref, type Ref } from 'vue'

export interface PageLoaderState {
    readonly index: number
    readonly blobUrl: Ref<string | null>
    readonly loading: Ref<boolean>
    readonly error: Ref<string | null>
    load(): Promise<void>
    dispose(): void
}

export function createPageLoader(index: number, url: string): PageLoaderState {
    const blobUrl = ref<string | null>(null)
    const loading = ref(false)
    const error = ref<string | null>(null)
    let active: AbortController | null = null

    async function load() {
        if (blobUrl.value || loading.value) return

        loading.value = true
        error.value = null

        const controller = new AbortController()
        active = controller

        try {
            const res = await fetch(url, { signal: controller.signal, credentials: 'include' })
            if (!res.ok) throw new Error(`HTTP ${res.status}`)

            const blob = await res.blob()

            // Check if aborted during blob read
            if (controller.signal.aborted) return

            blobUrl.value = URL.createObjectURL(blob)
        } catch (e) {
            if (active !== controller) return
            if (e instanceof DOMException && e.name === 'AbortError') {
                return // Silently ignore abort
            }
            error.value = e instanceof Error ? e.message : String(e)
        } finally {
            if (active === controller) {
                active = null
                loading.value = false
            }
        }
    }

    function dispose() {
        active?.abort()
        active = null
        loading.value = false
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
