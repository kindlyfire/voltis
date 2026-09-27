import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import type { MetadataLink } from '@/utils/api/metadata'
import ProviderLinkCard from './ProviderLinkCard.vue'

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

function text(link: MetadataLink) {
    return mount(ProviderLinkCard, {
        props: { contentId: 'c_1', link, title: 'Local' },
        global: {
            plugins: [[VueQueryPlugin, { queryClient: new QueryClient() }]],
            stubs: { ATooltip: { template: '<slot />' } },
        },
    }).text()
}

const date = (iso: string) => new Date(iso).toLocaleString()

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
})
