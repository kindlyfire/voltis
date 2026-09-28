import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { toasts } from '@/ui/useToast'
import type { Candidate, MetadataLink } from '@/utils/api/metadata'
import { apiFetch } from '@/utils/fetch'
import { ModalContainer } from '@/utils/modals'
import { addOverlays, settle } from '@/utils/modalTesting'
import { showProviderSearchModal } from './ProviderSearchModal.vue'

vi.mock('@/utils/fetch', async importOriginal => ({
    ...(await importOriginal<typeof import('@/utils/fetch')>()),
    apiFetch: vi.fn(),
}))

function candidate(id: string, title: string): Candidate {
    return {
        key: { provider: 'mangabaka', id },
        title,
        year: null,
        kind: 'manga',
        status: '',
        staff: [],
        cover_url: '',
        url: `https://mangabaka.org/${id}`,
        evaluation: { title: 0.8, exact: false, score: 0.8, eligible: false },
    }
}

const link: MetadataLink = {
    provider: 'mangabaka',
    label: 'MangaBaka',
    rev: 4,
    state: 'review',
    external_id: null,
    origin: null,
    entry: null,
    deleted: false,
    candidates: [candidate('1', 'Stored One'), candidate('2', 'Stored Two')],
    rejected: [],
    fetched_at: null,
    last_error: null,
    refresh_at: null,
    refresh_attempts: 0,
    refresh_error: null,
}

let requests: { url: string; body?: unknown }[]
let found: Promise<Candidate[]>
let rejected: Promise<void>
let wrapper: ReturnType<typeof mount>

function button(label: string) {
    const el = [...document.querySelectorAll<HTMLElement>('button')].find(
        b => b.textContent?.trim() === label
    )
    if (!el) throw new Error(`no button ${label}`)
    return el
}

async function click(label: string) {
    // Vue drops a click stamped no later than its listener was attached, which a frozen clock does.
    vi.advanceTimersByTime(1)
    button(label).click()
    await settle()
}

beforeEach(() => {
    vi.useFakeTimers()
    addOverlays()
    requests = []
    toasts.value = []
    found = Promise.resolve([candidate('3', 'Found Three')])
    rejected = Promise.resolve()
    vi.mocked(apiFetch).mockImplementation(async (url, init) => {
        requests.push({ url, body: init?.body && JSON.parse(init.body as string) })
        if (url.startsWith('/metadata/content/c_1/candidates?')) {
            return { data: await found }
        }
        if (url === '/metadata/content/c_1/reject') {
            await rejected
            return { links: [{ ...link, rev: 5 }] }
        }
        if (url === '/metadata/content/c_1/link') return { links: [{ ...link, rev: 6 }] }
        if (url === '/metadata/content/c_1/undo') return { links: [link] }
        throw new Error(`unexpected ${url}`)
    })
    wrapper = mount(ModalContainer, {
        attachTo: document.body,
        global: {
            plugins: [[VueQueryPlugin, { queryClient: new QueryClient() }]],
            stubs: { ATooltip: { template: '<slot />' } },
        },
    })
})

afterEach(() => {
    wrapper.unmount()
    vi.useRealTimers()
    document.body.innerHTML = ''
})

describe('ProviderSearchModal', () => {
    it('shows the stored candidates, and searches only when asked', async () => {
        showProviderSearchModal('c_1', link, 'Local')
        await settle()
        expect(document.body.textContent).toContain('Stored One')
        expect(document.body.textContent).toContain('Stored Two')
        expect(requests).toEqual([])

        await click('Search MangaBaka')
        expect(requests.map(r => r.url)).toEqual([
            '/metadata/content/c_1/candidates?provider=mangabaka&q=Local',
        ])
        expect(document.body.textContent).toContain('Found Three')
        expect(document.body.textContent).not.toContain('Stored One')
    })

    it('rejects the candidates shown', async () => {
        showProviderSearchModal('c_1', link, 'Local')
        await settle()
        await click('None of these')

        expect(requests).toEqual([
            {
                url: '/metadata/content/c_1/reject',
                body: { provider: 'mangabaka', external_ids: ['1', '2'], expect_rev: 4 },
            },
        ])
    })

    it('undoes from the toast, which outlives the modal', async () => {
        showProviderSearchModal('c_1', link, 'Local')
        await settle()
        await click('None of these')
        expect(document.body.textContent).not.toContain('Stored One')

        toasts.value[0]!.action!.onClick()
        await settle()
        expect(requests.at(-1)).toEqual({
            url: '/metadata/content/c_1/undo',
            body: { provider: 'mangabaka', expect_rev: 5 },
        })
        expect(toasts.value[1]!.message).toBe('Undone')
    })

    it('undoes a selection', async () => {
        showProviderSearchModal('c_1', link, 'Local')
        await settle()
        await click('Select')
        toasts.value[0]!.action!.onClick()
        await settle()
        expect(requests).toEqual([
            {
                url: '/metadata/content/c_1/link',
                body: { provider: 'mangabaka', external_id: '1', expect_rev: 4 },
            },
            { url: '/metadata/content/c_1/undo', body: { provider: 'mangabaka', expect_rev: 6 } },
        ])
    })

    it('offers Undo when dismissed before the decision is saved', async () => {
        let answer!: () => void
        rejected = new Promise(resolve => (answer = resolve))
        showProviderSearchModal('c_1', link, 'Local')
        await settle()
        // settle() would spin on the pending button's timers.
        vi.advanceTimersByTime(1)
        button('None of these').click()
        await vi.advanceTimersByTimeAsync(1)
        button('Cancel').click()
        await vi.advanceTimersByTimeAsync(1000)
        expect(document.body.textContent).not.toContain('Stored One')

        answer()
        await settle()
        expect(toasts.value[0]!.action!.label).toBe('Undo')
    })

    it('rejects nothing while searching or without results', async () => {
        let answer!: (c: Candidate[]) => void
        found = new Promise(resolve => (answer = resolve))
        showProviderSearchModal('c_1', { ...link, state: 'unmatched', candidates: [] }, 'Local')
        await settle()
        expect(button('None of these').hasAttribute('disabled')).toBe(true)

        answer([])
        await settle()
        expect(document.body.textContent).toContain('No results found.')
        expect(button('None of these').hasAttribute('disabled')).toBe(true)
    })

    it('offers no rejection for an ignored series', async () => {
        showProviderSearchModal('c_1', { ...link, state: 'ignored', candidates: [] }, 'Local')
        await settle()
        expect(document.body.textContent).toContain('Found Three')
        expect(() => button('None of these')).toThrow()
    })
})
