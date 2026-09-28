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
    title: 'One Piece',
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

function mountItem(content: Content, withSeries = true) {
    return mount(Item, {
        props: { content, series: withSeries ? series : null, toReadRoute: true },
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
        const w = mountItem(item())
        const { card, details } = links(w)
        expect(card.props('to')).toBe('/r/c_item?page=resume')
        expect(card.attributes('aria-label')).toBe('Read One Piece, Vol. 4, 3 of 10 read')
        expect(details.props('to')).toBe('/c_series')
        expect(w.find('.content-card__title').text()).toBe('One Piece')
        expect(w.find('.content-card__subtitle').text()).toBe('Vol. 4 · 3 / 10')
        expect(w.findComponent(ACover).props('progress')).toBeUndefined()
        expect(w.findComponent(ABadge).exists()).toBe(false)
    })

    it('names the status of a series item being read', () => {
        const w = mountItem(item('reading'))
        expect(links(w).card.attributes('aria-label')).toBe(
            'Read One Piece, Vol. 4, 3 of 10 read, Reading'
        )
        expect(w.findComponent(ACover).props('progress')).toBeCloseTo(0.4)
    })

    it('hides the progress with hideProgress', () => {
        useContentGridStore().getForKey('default').value = { hideProgress: true }
        const w = mountItem(item('reading'))
        expect(w.findComponent(ACover).props('progress')).toBeUndefined()
    })

    it('shows a standalone item', () => {
        const w = mountItem(item('reading', 'Dune'), false)
        const { card, details } = links(w)
        expect(card.attributes('aria-label')).toBe('Read Dune, Reading')
        expect(details.props('to')).toBe('/c_item')
        expect(w.find('.content-card__subtitle').exists()).toBe(false)
    })
})
