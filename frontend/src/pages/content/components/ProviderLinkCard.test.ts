import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { toasts } from '@/ui/useToast'
import type { MetadataLink } from '@/utils/api/metadata'
import { apiFetch } from '@/utils/fetch'
import ProviderLinkCard from './ProviderLinkCard.vue'

vi.mock('@/utils/fetch', async importOriginal => ({
    ...(await importOriginal<typeof import('@/utils/fetch')>()),
    apiFetch: vi.fn(),
}))

const linked: MetadataLink = {
    provider: 'mangabaka',
    label: 'MangaBaka',
    rev: 2,
    state: 'linked',
    external_id: '7',
    origin: 'manual',
    entry: null,
    deleted: false,
    candidates: [],
    rejected: [],
    fetched_at: '2026-09-20T10:00:00Z',
    last_error: null,
    refresh_at: '2026-09-28T10:00:00Z',
    refresh_attempts: 0,
    refresh_error: null,
}

function card(link: MetadataLink) {
    return mount(ProviderLinkCard, {
        props: { contentId: 'c_1', link, title: 'Local' },
        global: {
            plugins: [[VueQueryPlugin, { queryClient: new QueryClient() }]],
            stubs: { ATooltip: { template: '<slot />' } },
        },
    })
}

const text = (link: MetadataLink) => card(link).text()

const date = (iso: string) => new Date(iso).toLocaleString()

enableAutoUnmount(afterEach)
beforeEach(() => {
    toasts.value = []
})

const button = (c: ReturnType<typeof card>, label: string) =>
    c.findAll('button').find(b => b.text() === label)!

const reviewing: MetadataLink = {
    ...linked,
    state: 'review',
    external_id: null,
    candidates: [
        {
            key: { provider: 'mangabaka', id: '9' },
            title: 'Nine',
            year: null,
            kind: 'manga',
            status: '',
            staff: [],
            cover_url: '',
            url: '',
            evaluation: { title: 1, exact: true, score: 1, eligible: true },
        },
    ],
}

describe('ProviderLinkCard', () => {
    it('shows when the linked entry refreshes next', () => {
        expect(text(linked)).toContain(`Next refresh ${date(linked.refresh_at!)}`)
    })

    it('shows failing refreshes and unreadable data', () => {
        const t = text({
            ...linked,
            refresh_attempts: 2,
            refresh_error: 'timeout',
            last_error: 'no title',
        })
        expect(t).not.toContain('Next refresh')
        expect(t).toContain('Refreshing failed 2 times: timeout.')
        expect(t).toContain(`next try ${date(linked.refresh_at!)}`)
        expect(t).toContain("The stored MangaBaka data can't be read, so it's left out: no title")
    })

    it('offers no Undo for a refresh', async () => {
        vi.mocked(apiFetch).mockResolvedValue({ links: [linked] })
        await button(card(linked), 'Refresh').trigger('click')
        await flushPromises()
        expect(toasts.value[0]).toMatchObject({ message: 'Refreshed from MangaBaka' })
        expect(toasts.value[0]!.action).toBeUndefined()
    })

    it.each([
        ['Ignore', linked, 'ignore', {}],
        ['Accept', reviewing, 'link', { external_id: '9' }],
        ['Rematch', reviewing, 'rematch', {}],
    ])('undoes %s from its toast', async (label, link, action, extra) => {
        const requests: { url: string; body: unknown }[] = []
        vi.mocked(apiFetch).mockImplementation(async (url, init) => {
            requests.push({ url, body: JSON.parse(init!.body as string) })
            return { links: [{ ...link, rev: 3 }] }
        })
        await button(card(link), label).trigger('click')
        await flushPromises()
        toasts.value[0]!.action!.onClick()
        await flushPromises()
        expect(requests).toEqual([
            {
                url: `/metadata/content/c_1/${action}`,
                body: { provider: 'mangabaka', ...extra, expect_rev: 2 },
            },
            { url: '/metadata/content/c_1/undo', body: { provider: 'mangabaka', expect_rev: 3 } },
        ])
    })

    it('offers Undo when unmounted before the decision is saved', async () => {
        let answer!: (v: unknown) => void
        vi.mocked(apiFetch).mockReturnValue(new Promise(resolve => (answer = resolve)))
        const c = card(linked)
        await button(c, 'Ignore').trigger('click')
        c.unmount()
        answer({ links: [{ ...linked, state: 'ignored', rev: 3 }] })
        await flushPromises()
        expect(toasts.value[0]!.action!.label).toBe('Undo')
    })
})
