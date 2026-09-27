import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createHead } from '@unhead/vue/client'
import { flushPromises, mount, RouterLinkStub } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ASelect from '@/ui/ASelect.vue'
import type { ReviewAction, ReviewItem } from '@/utils/api/metadata'
import { apiFetch } from '@/utils/fetch'
import { addOverlays } from '@/utils/modalTesting'
import MetadataPage from './MetadataPage.vue'

vi.mock('@/utils/fetch', async importOriginal => ({
    ...(await importOriginal<typeof import('@/utils/fetch')>()),
    apiFetch: vi.fn(),
}))

function item(id: string, title: string, rev: number): ReviewItem {
    const candidate = {
        key: { provider: 'mangabaka', id: `e_${id}` },
        title: `${title} upstream`,
        year: 2020,
        kind: 'manga',
        status: 'releasing',
        staff: [],
        cover_url: '',
        url: `https://mangabaka.org/e_${id}`,
        evaluation: { title: 1, exact: true, score: 1, eligible: true },
    }
    return {
        content: { id, title, library_id: 'l1' } as ReviewItem['content'],
        local_title: title,
        link: {
            provider: 'mangabaka',
            label: 'MangaBaka',
            rev,
            state: 'review',
            external_id: null,
            origin: null,
            entry: null,
            deleted: false,
            candidates: [candidate, { ...candidate, key: { provider: 'mangabaka', id: 'other' } }],
            rejected: [],
            fetched_at: null,
            last_error: null,
            refresh_at: null,
            refresh_attempts: 0,
            refresh_error: null,
        },
    }
}

let resolved: ReviewAction[][]
let requests: { url: string; body: unknown }[]
let reviewQueries: URLSearchParams[]
let wrapper: ReturnType<typeof mount>
let libraries: { id: string; name: string; settings: { auto_match: boolean } }[]

beforeEach(() => {
    addOverlays()
    resolved = []
    requests = []
    reviewQueries = []
    libraries = [
        { id: 'l1', name: 'Manga', settings: { auto_match: true } },
        { id: 'l2', name: 'Books', settings: { auto_match: false } },
    ]
    vi.mocked(apiFetch).mockImplementation(async (url, init) => {
        if (url === '/libraries') return libraries
        if (url === '/metadata/config') {
            return { fields: [], providers: [{ name: 'mangabaka', label: 'MangaBaka' }] }
        }
        if (url === '/metadata/summary') {
            return {
                libraries: [{ library_id: 'l1', review: 2, unmatched: 0, failed: 0 }],
                providers: [
                    {
                        provider: 'mangabaka',
                        failing: 2,
                        undecodable: 0,
                        last_fetched: '2026-09-27T10:00:00Z',
                    },
                ],
                worker: {
                    activity: 'matching',
                    library_id: 'l1',
                    paused: false,
                    matched: { linked: 3, review: 2, unmatched: 1, failed: 0, skipped: 0 },
                    refreshed: { refreshed: 0, failed: 0 },
                    match_pass: {
                        counts: { linked: 3, review: 2, unmatched: 1, failed: 0, skipped: 0 },
                        finished: null,
                    },
                    refresh_pass: {
                        counts: { refreshed: 0, failed: 0 },
                        started: null,
                        finished: null,
                    },
                },
            }
        }
        if (url === '/settings') {
            if (init?.method === 'POST')
                requests.push({ url, body: JSON.parse(init.body as string) })
            return []
        }
        if (url === '/metadata/match') {
            requests.push({ url, body: JSON.parse(init!.body as string) })
            return { ok: true }
        }
        if (url.startsWith('/metadata/review?')) {
            reviewQueries.push(new URLSearchParams(url.split('?')[1]))
            return { data: [item('c_1', 'Frieren', 3), item('c_2', 'Emma', 5)], total: 2 }
        }
        if (url === '/metadata/review/resolve') {
            const { items } = JSON.parse(init!.body as string)
            resolved.push(items)
            if (items[0].action === 'reject') {
                return { results: items.map((i: ReviewAction) => ({ ...i, ok: true })) }
            }
            return {
                results: [
                    { content_id: 'c_1', provider: 'mangabaka', ok: true },
                    {
                        content_id: 'c_2',
                        provider: 'mangabaka',
                        ok: false,
                        error: 'changed since it was read',
                    },
                ],
            }
        }
        throw new Error(`unexpected ${url}`)
    })
    wrapper = mount(MetadataPage, {
        attachTo: document.body,
        global: {
            plugins: [[VueQueryPlugin, { queryClient: new QueryClient() }], createHead()],
            stubs: { RouterLink: RouterLinkStub, ATooltip: { template: '<slot />' } },
        },
    })
})

afterEach(() => {
    wrapper.unmount()
    document.body.innerHTML = ''
})

function button(label: string) {
    const el = wrapper.findAll('button').find(b => b.text() === label)
    if (!el) throw new Error(`no button ${label}`)
    return el
}

describe('MetadataPage', () => {
    it('accepts the selected series and keeps the ones that failed, with their error', async () => {
        await flushPromises()
        expect(wrapper.text()).toContain('Needs review (2)')
        expect(wrapper.text()).toContain('Frieren upstream')

        await wrapper.find('thead input[type="checkbox"]').setValue(true)
        await button('Accept selected').trigger('click')
        await flushPromises()

        expect(resolved).toEqual([
            [
                {
                    content_id: 'c_1',
                    provider: 'mangabaka',
                    action: 'link',
                    external_id: 'e_c_1',
                    expect_rev: 3,
                },
                {
                    content_id: 'c_2',
                    provider: 'mangabaka',
                    action: 'link',
                    external_id: 'e_c_2',
                    expect_rev: 5,
                },
            ],
        ])
        const rows = wrapper.findAll('tbody tr')
        expect(rows[0].text()).not.toContain('changed since it was read')
        expect(rows[1].text()).toContain('changed since it was read')
        // The failed row stays selectable for another try; the rest were cleared.
        expect(wrapper.text()).toContain('0 selected')
    })

    it('rejects the candidates of the selected series', async () => {
        await flushPromises()
        await wrapper.find('thead input[type="checkbox"]').setValue(true)
        await button('Reject selected').trigger('click')
        await flushPromises()

        expect(resolved).toEqual([
            ['c_1', 'c_2'].map((id, i) => ({
                content_id: id,
                provider: 'mangabaka',
                action: 'reject',
                external_ids: [`e_${id}`, 'other'],
                expect_rev: [3, 5][i],
            })),
        ])
    })

    it('searches titles and filters failures', async () => {
        await flushPromises()
        vi.useFakeTimers()
        try {
            await wrapper.find('input[type="search"]').setValue(' frier ')
            await vi.advanceTimersByTimeAsync(300)
            await flushPromises()
            expect(reviewQueries.at(-1)?.get('q')).toBe('frier')

            await button('No match (0)').trigger('click')
            await wrapper
                .findAll('input[type="checkbox"]')
                .find(c => c.element.closest('label')?.textContent?.includes('Errors only'))!
                .setValue(true)
            await flushPromises()
            const last = reviewQueries.at(-1)!
            expect(Object.fromEntries(last)).toMatchObject({
                tab: 'unmatched',
                q: 'frier',
                failed: 'true',
            })
            // The review tab has no such filter.
            await button('Needs review (2)').trigger('click')
            await flushPromises()
            expect(reviewQueries.at(-1)?.get('tab')).toBe('review')
            expect(reviewQueries.at(-1)?.has('failed')).toBe(false)
        } finally {
            vi.useRealTimers()
        }
    })

    it('shows what the worker does, matches now, and pauses matching', async () => {
        await flushPromises()
        const chip = wrapper.find('[aria-live="polite"]')
        expect(chip.text()).toBe('Matching Manga…')
        expect(wrapper.text()).toContain('MangaBaka: 2 refreshes failing')

        await button('Details').trigger('click')
        await flushPromises()
        const details = document.querySelector('[role="dialog"]')!.textContent
        expect(details).toContain('Last matchRunning: 3 linked, 2 review, 1 unmatched')
        expect(details).toContain('Last refresh—')
        expect(details).toContain('Since start3 linked, 2 review, 1 unmatched, 0 refreshed')
        expect(details).toContain('MangaBaka fetched')

        await button('Match now').trigger('click')
        await wrapper.find('input[role="switch"]').setValue(true)
        await flushPromises()
        expect(requests).toEqual([
            { url: '/metadata/match', body: {} },
            { url: '/settings', body: { 'metadata.matching_paused': true } },
        ])
    })

    it('disables Match now where nothing matches automatically', async () => {
        await flushPromises()
        expect(button('Match now').attributes('aria-disabled')).toBeUndefined()
        wrapper.findComponent(ASelect).vm.$emit('update:modelValue', 'l2')
        await flushPromises()
        expect(button('Match now').attributes('aria-disabled')).toBe('true')
        await button('Match now').trigger('click')
        await flushPromises()
        expect(requests).toEqual([])
        // Nor for all libraries, when none does.
        libraries[0].settings.auto_match = false
        wrapper.findComponent(ASelect).vm.$emit('update:modelValue', null)
        await flushPromises()
        expect(button('Match now').attributes('aria-disabled')).toBe('true')
    })
})
