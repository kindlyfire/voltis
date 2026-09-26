import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import type { FolderListing, ResolvedPath } from '@/utils/api/types'
import { apiFetch, RequestError } from '@/utils/fetch'
import { useFolderBrowser } from './useFolderBrowser'

vi.mock('@/utils/fetch', async importOriginal => ({
    ...(await importOriginal<typeof import('@/utils/fetch')>()),
    apiFetch: vi.fn(),
}))

type Pending = { url: string; resolve: (v: unknown) => void; reject: (e: unknown) => void }
let pending: Pending[] = []

function listing(path: string, names: string[] = []): FolderListing {
    const parent = path === '/' ? null : path.slice(0, path.lastIndexOf('/')) || '/'
    return {
        path,
        parent,
        entries: names.map(name => ({ name, path: `${path}/${name}`, symlink: false })),
        truncated: false,
    }
}

function take(url: string) {
    const i = pending.findIndex(p => p.url === url)
    if (i < 0) throw new Error(`no pending request for ${url}`)
    return pending.splice(i, 1)[0]!
}

function respond(url: string, value: unknown) {
    take(url).resolve(value)
    return flushPromises()
}

function fail(url: string, error: string) {
    take(url).reject(new RequestError('Request failed', { json: { error } }))
    return flushPromises()
}

const listUrl = (path: string) => `/fs/list?${new URLSearchParams({ path })}`

function setup() {
    let browser!: ReturnType<typeof useFolderBrowser>
    const Host = defineComponent({
        setup() {
            browser = useFolderBrowser()
            return () => null
        },
    })
    const queryClient = new QueryClient()
    mount(Host, { global: { plugins: [[VueQueryPlugin, { queryClient }]] } })
    return browser
}

enableAutoUnmount(afterEach)

beforeEach(() => {
    pending = []
    vi.mocked(apiFetch).mockImplementation(
        url => new Promise((resolve, reject) => pending.push({ url, resolve, reject }))
    )
})

describe('useFolderBrowser', () => {
    it('ignores responses for folders the user already left', async () => {
        const b = setup()
        b.navigate('/a')
        await flushPromises()
        b.navigate('/b/')
        await flushPromises()
        expect(b.target.value).toBeNull()

        await respond(listUrl('/b'), listing('/b'))
        await respond(listUrl('/a'), listing('/a'))
        expect(b.displayed.value?.path).toBe('/b')
        expect(b.target.value).toBe('/b')
    })

    it('keeps the last folder after a failed navigation', async () => {
        const b = setup()
        b.navigate('/a')
        await flushPromises()
        await respond(listUrl('/a'), listing('/a', ['x']))

        b.navigate('/a/x')
        await flushPromises()
        await fail(listUrl('/a/x'), 'Permission denied: /a/x')
        expect(b.error.value).toBe('Permission denied: /a/x')
        expect(b.displayed.value?.path).toBe('/a')
        expect(b.target.value).toBe('/a')
    })

    it('reports a folder reached through a symlink', async () => {
        const b = setup()
        b.navigate('/a/link')
        await flushPromises()
        await respond(listUrl('/a/link'), listing('/real'))
        expect(b.resolvedTo.value).toBe('/real')
        expect(b.target.value).toBe('/real')
    })

    it('leaves the path bar without navigating for an empty draft or the loaded folder', async () => {
        const b = setup()
        b.navigate('/a')
        await flushPromises()
        await respond(listUrl('/a'), listing('/a'))
        for (const draft of ['', '/a/']) {
            b.startEditing()
            b.draft.value = draft
            expect(b.submitDraft()).toBe(false)
            expect(b.editing.value).toBe(false)
        }
        await flushPromises()
        expect(pending).toEqual([])

        // While another folder loads, the one still shown is a real destination.
        b.navigate('/b')
        await flushPromises()
        b.draft.value = '/a'
        expect(b.submitDraft()).toBe(true)
    })

    it('opens at the nearest existing folder, unless the user moved first', async () => {
        const b = setup()
        b.open('/media/old')
        await flushPromises()
        expect(b.loading.value).toBe(true)
        const resolved: ResolvedPath = {
            input: '/media/old',
            path: '/media',
            fallback_from: '/media/old',
        }
        await respond('/fs/resolve', { results: [resolved] })
        expect(b.requested.value).toBe('/media')
        expect(b.notice.value).toBe('/media/old no longer exists; showing /media')

        b.open('/elsewhere')
        await flushPromises()
        b.navigate('/mine')
        await respond('/fs/resolve', { results: [{ input: '/elsewhere', path: '/elsewhere' }] })
        expect(b.requested.value).toBe('/mine')
        expect(b.notice.value).toBeUndefined()
    })

    it('goes up from a folder that failed to open, and re-edits the requested path', async () => {
        const b = setup()
        b.navigate('/a/secret')
        await flushPromises()
        await fail(listUrl('/a/secret'), 'Permission denied: /a/secret')
        expect(b.displayed.value).toBeNull()
        expect(b.parent.value).toBe('/a')

        b.startEditing()
        expect(b.draft.value).toBe('/a/secret')
    })

    it('has no parent for a rejected relative path', async () => {
        const b = setup()
        b.navigate('books')
        await flushPromises()
        await fail(listUrl('books'), 'Path must be absolute')
        expect(b.parent.value).toBeNull()
    })

    it('retries when the failed path is submitted again', async () => {
        const b = setup()
        b.navigate('/x')
        await flushPromises()
        await fail(listUrl('/x'), 'Folder does not exist: /x')
        b.navigate('/x/')
        await flushPromises()
        expect(pending.map(p => p.url)).toEqual([listUrl('/x')])
    })
})
