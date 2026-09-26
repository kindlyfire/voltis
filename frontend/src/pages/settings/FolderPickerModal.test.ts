import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { FolderListing } from '@/utils/api/types'
import { apiFetch } from '@/utils/fetch'
import { hasOpenModal, ModalContainer, Modals } from '@/utils/modals'
import { addOverlays, settle, TestDialog } from '@/utils/modalTesting'
import { showFolderPicker } from './FolderPickerModal.vue'

vi.mock('@/utils/fetch', async importOriginal => ({
    ...(await importOriginal<typeof import('@/utils/fetch')>()),
    apiFetch: vi.fn(),
}))

const listings: Record<string, FolderListing> = {
    '/a': {
        path: '/a',
        parent: '/',
        entries: [
            { name: 'x', path: '/a/x', symlink: false },
            { name: 'y', path: '/a/y', symlink: false },
            { name: 'link', path: '/a/link', symlink: true },
        ],
        truncated: false,
    },
}

function dialogs() {
    return [...document.querySelectorAll('[role="dialog"]')]
}

function button(label: string) {
    const el = [...document.querySelectorAll<HTMLElement>('button')].find(
        b => b.getAttribute('aria-label') === label || b.textContent?.trim() === label
    )
    if (!el) throw new Error(`no button ${label}`)
    return el
}

function checkbox(name: string) {
    const el = [...document.querySelectorAll<HTMLInputElement>('input[type=checkbox]')].find(i =>
        i.closest('label')?.textContent?.includes(`Select ${name}`)
    )
    if (!el) throw new Error(`no checkbox for ${name}`)
    return el
}

function pressEscape(target: Element) {
    target.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    )
}

let wrapper: ReturnType<typeof mount>

beforeEach(() => {
    vi.useFakeTimers()
    // Not implemented by jsdom.
    Element.prototype.scrollIntoView = () => {}
    addOverlays()
    vi.mocked(apiFetch).mockImplementation(async (url, init) => {
        if (url === '/libraries') return []
        if (url === '/fs/roots') return { mounts: [{ path: '/' }] }
        if (url === '/fs/resolve') {
            const { paths } = JSON.parse(init!.body as string)
            const real = (p: string) => (p === '/a/link' ? '/real' : p)
            return { results: paths.map((p: string) => ({ input: p, path: real(p) })) }
        }
        const path = new URLSearchParams(url.split('?')[1]).get('path')!
        return listings[path] ?? { path, parent: '/', entries: [], truncated: false }
    })
    const queryClient = new QueryClient()
    wrapper = mount(ModalContainer, {
        attachTo: document.body,
        global: {
            plugins: [[VueQueryPlugin, { queryClient }]],
            stubs: { ATooltip: { template: '<slot />' } },
        },
    })
})

afterEach(() => {
    wrapper.unmount()
    vi.useRealTimers()
    document.body.innerHTML = ''
})

describe('FolderPickerModal', () => {
    it('resolves null on cancel', async () => {
        const result = showFolderPicker({ otherSourcePaths: [] })
        await settle()
        button('Cancel').click()
        await settle()
        expect(await result).toBeNull()
    })

    it('closes only itself on Escape, but not while editing the path', async () => {
        const parentClosed = vi.fn()
        Modals.show(TestDialog).then(parentClosed)
        await settle()
        const result = vi.fn()
        showFolderPicker({ initialPath: '/a', otherSourcePaths: [] }).then(result)
        await settle()

        button('Type a path').click()
        await settle()
        const input = document.activeElement!
        expect(input.tagName).toBe('INPUT')
        pressEscape(input)
        await settle()
        expect(document.querySelector('input:not([type])')).toBeNull()
        expect(result).not.toHaveBeenCalled()
        expect(dialogs()).toHaveLength(2)

        pressEscape(document.activeElement!)
        await settle()
        expect(result).toHaveBeenCalledWith(null)
        expect(parentClosed).not.toHaveBeenCalled()
        expect(dialogs()).toHaveLength(1)
        expect(hasOpenModal.value).toBe(true)
    })

    it('keeps the selection across navigation, and removes from the basket', async () => {
        const result = showFolderPicker({ initialPath: '/a', multiple: true, otherSourcePaths: [] })
        await settle()
        checkbox('x').click()
        button('Select this folder').click()
        await settle()
        button('2 selected').click()
        await settle()

        button('Home').click()
        await settle()
        button('Remove /a/x').click()
        await settle()
        expect(button('1 selected')).toBeTruthy()

        button('Add folder').click()
        await settle()
        expect(await result).toEqual(['/a'])
    })

    it('selects with checkboxes without navigating, returning resolved paths', async () => {
        const result = showFolderPicker({ initialPath: '/a', multiple: true, otherSourcePaths: [] })
        await settle()

        for (const name of ['x', 'link']) {
            checkbox(name).click()
            await settle()
        }
        expect(vi.mocked(apiFetch).mock.calls.some(([url]) => url.includes('%2Fa%2Fx'))).toBe(false)
        button('Add 2 folders').click()
        await settle()
        expect(await result).toEqual(['/a/x', '/real'])
    })
})
