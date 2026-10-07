import type { QueryClient } from '@tanstack/vue-query'
import { useToast } from '@/ui/useToast'
import { miscApi } from './api/misc'
import { ws } from './ws'

let toast: { isOpen: () => boolean } | undefined

function prompt() {
    if (toast?.isOpen()) return
    toast = useToast().show({
        message: 'A new version is available.',
        tone: 'info',
        duration: Infinity,
        action: { label: 'Reload', altText: 'Reload the page', onClick: () => location.reload() },
    })
}

/**
 * Prompts a reload when the server reports a different frontend build after a reconnect.
 * `buildId` defaults to the meta vite.build-id.ts injects, absent in dev.
 */
export function watchForNewBuild(
    queryClient: QueryClient,
    buildId = document.querySelector<HTMLMetaElement>('meta[name="voltis-build"]')?.content
): void {
    if (!buildId) return
    ws.on('$open', async () => {
        const info = await miscApi.info().catch(() => null)
        if (!info) return
        queryClient.setQueryData(['misc', 'info'], info)
        // Once dismissed, only a preload error shows it again this page load.
        if (info.web_build && info.web_build !== buildId && !toast) prompt()
    })
    // A missing lazy chunk means the build it belongs to is gone.
    window.addEventListener('vite:preloadError', e => {
        e.preventDefault()
        prompt()
    })
}
