import { mount, RouterLinkStub } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'
import ABadge from '@/ui/ABadge.vue'
import ACover from '@/ui/ACover.vue'
import type { Content, ReadingStatus } from '@/utils/api/types'
import Item from './Item.vue'
import { useContentGridStore } from './store'

const series = {
    id: 'c_series',
    type: 'comic_series',
    title: 'Harbor Lights',
    children_count: 10,
    unread_children_count: 7,
    user_data: null,
} as unknown as Content

function item(status?: ReadingStatus, title = 'Vol. 4') {
    return {
        id: 'c_item',
        type: 'comic',
        title,
        file_data: {},
        user_data: status ? { status, progress: { current_page: 12, progress_percent: 40 } } : null,
    } as unknown as Content
}

function mountItem(
    content: Content,
    withSeries = true,
    extra: { highlightReading?: boolean } = {}
) {
    return mount(Item, {
        props: { content, series: withSeries ? series : null, toReadRoute: true, ...extra },
        global: { stubs: { RouterLink: RouterLinkStub, ATooltip: { template: '<slot />' } } },
    })
}

function links(w: ReturnType<typeof mountItem>) {
    const [card, details] = w.findAllComponents(RouterLinkStub)
    return { card: card!, details: details! }
}

beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
})

describe('ContentGrid Item', () => {
    it('shows an unstarted series item', () => {
        const w = mountItem(item(), true, { highlightReading: true })
        const { card, details } = links(w)
        expect(card.props('to')).toBe('/r/c_item?page=resume')
        expect(card.attributes('aria-label')).toBe('Read Harbor Lights, Vol. 4, 3 of 10 read')
        expect(details.props('to')).toBe('/c_series')
        expect(w.find('.content-card__title').text()).toBe('Harbor Lights')
        expect(w.find('.content-card__subtitle').text()).toBe('Vol. 4 · 3 / 10')
        expect(w.findComponent(ACover).props('progress')).toBeUndefined()
        expect(w.findComponent(ABadge).exists()).toBe(false)
        expect(w.classes()).not.toContain('reading')
    })

    it('names the status of a series item being read', async () => {
        const w = mountItem(item('reading'), true, { highlightReading: true })
        expect(links(w).card.attributes('aria-label')).toBe(
            'Read Harbor Lights, Vol. 4, 3 of 10 read, Reading'
        )
        expect(w.findComponent(ACover).props('progress')).toBeCloseTo(0.4)
        expect(w.classes()).toContain('reading')
        await w.setProps({ selecting: true })
        expect(w.classes()).not.toContain('reading')
    })

    it('hides progress and highlight via settings', () => {
        useContentGridStore().getForKey('default').value = {
            hideProgress: true,
            hideReadingHighlight: true,
        }
        const w = mountItem(item('reading'), true, { highlightReading: true })
        expect(w.findComponent(ACover).props('progress')).toBeUndefined()
        expect(w.classes()).not.toContain('reading')
    })

    it('shows a standalone item', () => {
        const w = mountItem(item('reading', 'Tidewater'), false)
        const { card, details } = links(w)
        expect(card.attributes('aria-label')).toBe('Read Tidewater, Reading')
        expect(details.props('to')).toBe('/c_item')
        expect(w.find('.content-card__subtitle').exists()).toBe(false)
        expect(w.classes()).not.toContain('reading')
    })
})
