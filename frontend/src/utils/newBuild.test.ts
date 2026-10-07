import { QueryClient } from '@tanstack/vue-query'
import { expect, it, vi } from 'vitest'
import { dismissToast, toasts } from '@/ui/useToast'
import { type Info, miscApi } from '@/utils/api/misc'
import { watchForNewBuild } from '@/utils/newBuild'

const ws = vi.hoisted(() => ({ onOpen: async () => {} }))
vi.mock('@/utils/ws', () => ({
    ws: { on: (_type: string, handler: () => Promise<void>) => (ws.onOpen = handler) },
}))

it('prompts once per new build until dismissed, and again on a preload error', async () => {
    const info = vi.spyOn(miscApi, 'info')
    const client = new QueryClient()
    watchForNewBuild(client, 'a')
    const open = (web_build: string) => {
        info.mockResolvedValue({ web_build } as Info)
        return ws.onOpen()
    }
    const preloadError = () => {
        const e = new Event('vite:preloadError', { cancelable: true })
        window.dispatchEvent(e)
        return e
    }

    await open('a')
    await open('')
    expect(toasts.value).toHaveLength(0)
    expect(client.getQueryData(['misc', 'info'])).toEqual({ web_build: '' })

    await open('b')
    await open('b')
    expect(toasts.value).toHaveLength(1)
    const [toast] = toasts.value
    expect(toast).toMatchObject({ tone: 'info', duration: Infinity, action: { label: 'Reload' } })

    dismissToast(toast!.id)
    await open('b')
    expect(toasts.value.filter(t => t.open)).toHaveLength(0)

    expect(preloadError().defaultPrevented).toBe(true)
    preloadError()
    expect(toasts.value.filter(t => t.open)).toHaveLength(1)
})
