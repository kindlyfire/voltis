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
    completed_children_count: 2,
    dropped_children_count: 1,
    user_data: null,
} as unknown as Content

const TooltipStub = { props: ['disabled'], template: '<div><slot /><slot name="content" /></div>' }

function item(status?: ReadingStatus, title = 'Vol. 4', order_parts = [4]) {
    return {
        id: 'c_item',
        type: 'comic',
        title,
        order_parts,
        file_data: {},
        user_data: status ? { status, progress: { current_page: 12, progress_percent: 40 } } : null,
    } as unknown as Content
}

function mountItem(
    content: Content,
    withSeries = true,
    extra: { highlightReading?: boolean; parent?: Content } = {}
) {
    return mount(Item, {
        props: { content, series: withSeries ? series : null, toReadRoute: true, ...extra },
        global: { stubs: { RouterLink: RouterLinkStub, ATooltip: TooltipStub } },
    })
}

function links(w: ReturnType<typeof mountItem>) {
    const [card, details] = w.findAllComponents(RouterLinkStub)
    return { card: card!, details: details! }
}

// The details button has its own tooltip.
function titleTooltip(w: ReturnType<typeof mountItem>) {
    return w.findAllComponents(TooltipStub).find(t => t.find('.content-card__text').exists())!
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
        expect(card.attributes('aria-label')).toBe(
            'Read Harbor Lights, Vol. 4, 2/10 read · 1 dropped'
        )
        expect(details.props('to')).toBe('/c_series')
        expect(w.find('.content-card__title').text()).toBe('Harbor Lights')
        expect(w.find('.content-card__subtitle').text()).toBe('Volume 4 · 2/10 read · 1 dropped')
        expect(titleTooltip(w).props('disabled')).toBe(false)
        expect(w.findComponent(ACover).props('progress')).toBeUndefined()
        expect(w.findComponent(ABadge).exists()).toBe(false)
        expect(w.classes()).not.toContain('reading')
    })

    it('names the status of a series item being read', async () => {
        const w = mountItem(item('reading'), true, { highlightReading: true })
        expect(links(w).card.attributes('aria-label')).toBe(
            'Read Harbor Lights, Vol. 4, 2/10 read · 1 dropped, Reading'
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

    it('shows a series item by its number', () => {
        const parent = { type: 'book_series', title: 'Ember Saga', meta: {} } as unknown as Content
        const full = 'Ember Saga, Vol. 3: The Long Road'
        const w = mountItem(item(undefined, full, [3]), false, { parent })
        expect(w.find('.content-card__title').text()).toBe('Volume 3')
        expect(w.find('.content-card__subtitle').text()).toBe('The Long Road')
        const tooltip = titleTooltip(w)
        expect(tooltip.props('disabled')).toBe(false)
        expect(tooltip.text()).toContain(full)
        expect(links(w).card.attributes('aria-label')).toBe(`Read ${full}`)
    })

    it('badges new volumes', () => {
        const w = mount(Item, {
            props: { content: item(), series, toReadRoute: true, isNew: true },
            global: { stubs: { RouterLink: RouterLinkStub, ATooltip: { template: '<slot />' } } },
        })
        expect(w.find('.content-card__new').text()).toBe('New')
        expect(links(w).card.attributes('aria-label')).toBe(
            'Read Harbor Lights, Vol. 4, 2/10 read · 1 dropped, New'
        )

        const completed = {
            ...series,
            new_children_count: 2,
            user_data: { status: 'completed', progress: {} },
        } as unknown as Content
        const s = mount(Item, {
            props: { content: completed },
            global: { stubs: { RouterLink: RouterLinkStub, ATooltip: { template: '<slot />' } } },
        })
        expect(s.find('.content-card__new').text()).toBe('2 new')
    })

    it('labels a caught-up series being read', () => {
        const caughtUp = {
            ...series,
            unread_children_count: 0,
            completed_children_count: 9,
            user_data: { status: 'reading', progress: {} },
        } as unknown as Content
        const w = mount(Item, {
            props: { content: caughtUp },
            global: { stubs: { RouterLink: RouterLinkStub, ATooltip: { template: '<slot />' } } },
        })
        expect(links(w).card.attributes('aria-label')).toContain('Reading · Caught up')
    })
})
