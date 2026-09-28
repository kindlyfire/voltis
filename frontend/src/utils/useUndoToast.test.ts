import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { toasts } from '@/ui/useToast'
import { apiFetch, RequestError } from './fetch'
import { useUndoToast } from './useUndoToast'

vi.mock('./fetch', async importOriginal => ({
    ...(await importOriginal<typeof import('./fetch')>()),
    apiFetch: vi.fn(),
}))

enableAutoUnmount(afterEach)

let undoToast: ReturnType<typeof useUndoToast>
let requests: { url: string; body: unknown }[]

beforeEach(() => {
    toasts.value = []
    requests = []
    vi.mocked(apiFetch).mockImplementation(async (url, init) => {
        const body = JSON.parse(init!.body as string)
        requests.push({ url, body })
        if (url === '/metadata/content/c_1/undo') return { links: [] }
        if (url === '/metadata/content/c_2/undo') {
            throw new RequestError('Request failed: 409 Conflict', {
                json: { error: 'Can no longer undo; it expired or changed since' },
            })
        }
        if (url === '/metadata/review/resolve') {
            return {
                results: body.items.map((i: { content_id: string }) => ({
                    ...i,
                    ok: i.content_id === 'c_1',
                })),
            }
        }
        throw new Error(`unexpected ${url}`)
    })
    mount(
        defineComponent({
            setup() {
                undoToast = useUndoToast()
                return () => null
            },
        }),
        { global: { plugins: [[VueQueryPlugin, { queryClient: new QueryClient() }]] } }
    )
})

const item = (id: string, rev: number) => ({ content_id: id, provider: 'mangabaka', rev })

async function clickUndo() {
    toasts.value[0]!.action!.onClick()
    await flushPromises()
    return toasts.value.slice(1).map(t => ({ message: t.message, tone: t.tone }))
}

describe('useUndoToast', () => {
    it('offers no undo without items', () => {
        undoToast('Refreshed', [])
        expect(toasts.value[0]!.action).toBeUndefined()
    })

    it('undoes one decision', async () => {
        undoToast('Linked', [item('c_1', 3)])
        expect(toasts.value[0]!.action!.label).toBe('Undo')
        expect(await clickUndo()).toEqual([{ message: 'Undone', tone: undefined }])
        expect(requests).toEqual([
            {
                url: '/metadata/content/c_1/undo',
                body: { provider: 'mangabaka', expect_rev: 3 },
            },
        ])
    })

    it('says why it could not undo', async () => {
        undoToast('Linked', [item('c_2', 3)])
        expect(await clickUndo()).toEqual([
            {
                message: "Couldn't undo: Can no longer undo; it expired or changed since",
                tone: 'danger',
            },
        ])
    })

    it('undoes several decisions at once, counting failures', async () => {
        undoToast('2 series done', [item('c_1', 3), item('c_2', 5)])
        expect(await clickUndo()).toEqual([{ message: '1 undone, 1 failed', tone: 'danger' }])
        expect(requests).toEqual([
            {
                url: '/metadata/review/resolve',
                body: {
                    items: [
                        { content_id: 'c_1', provider: 'mangabaka', action: 'undo', expect_rev: 3 },
                        { content_id: 'c_2', provider: 'mangabaka', action: 'undo', expect_rev: 5 },
                    ],
                },
            },
        ])
    })
})
